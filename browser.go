package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type stateFunc func(*feedBrowser) stateFunc

type feedBrowser struct {
	baseUrl      string
	username     string
	password     string
	numRetries   uint
	logger       *log.Logger
	broswer      *rod.Browser
	page         *rod.Page
	imageReqFeed chan string
	ctx          context.Context
	imageLoadWG  *sync.WaitGroup
	hjRouter     *rod.HijackRouter
}

func newFeedBrowser(
	logger *log.Logger,
	browser *rod.Browser,
	imageReqFeed chan string,
	numRetries uint,
	username, password string,
) *feedBrowser {
	return &feedBrowser{
		baseUrl:      "https://x.com",
		username:     username,
		password:     password,
		numRetries:   numRetries,
		logger:       logger,
		broswer:      browser,
		imageReqFeed: imageReqFeed,
		imageLoadWG:  &sync.WaitGroup{},
	}
}

func (fb *feedBrowser) run(ctx context.Context) {
	fb.ctx = ctx
	for state := navigateToRoot; state != nil; {
		state = state(fb)
	}
}

func (fb *feedBrowser) errorf(format string, args ...interface{}) stateFunc {
	fb.logger.Errorf(format, args...)
	return nil
}

func navigateToRoot(fb *feedBrowser) stateFunc {
	page := fb.broswer.
		MustPage(fb.baseUrl).
		MustWaitIdle()
	fb.page = page

	url := page.MustInfo().URL
	if strings.Contains(url, "home") {
		return navigateToLikedTweets
	}

	return navigateToLogin
}

func navigateToLogin(fb *feedBrowser) stateFunc {
	_ = fb.page.MustNavigate(fb.baseUrl + "/i/flow/login").
		MustWaitIdle()

	_, err := fb.page.ElementR("span", "Sign in to X")
	if err != nil {
		return fb.errorf("failed to navigate to login")
	}

	return login
}

func login(fb *feedBrowser) stateFunc {
	page := fb.page

	_ = page.MustElement("input[name=text]").
		MustInput(fb.username)

	_ = page.MustElement("button.css-175oi2r:nth-child(6)").
		MustClick()

	_ = page.MustElement("input[name=password]").
		MustInput(fb.password)

	wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)

	_ = page.MustElement(".r-19yznuf").
		MustClick()

	wait()

	url := page.MustWaitIdle().
		MustInfo().URL
	if strings.Contains(url, "home") {
		return navigateToLikedTweets
	} else {
		return fb.errorf("not at home url: %s", url)
	}
}

func navigateToLikedTweets(fb *feedBrowser) stateFunc {
	hjRouter := fb.page.HijackRequests()
	hjRouter.Add(
		"https://pbs\\.twimg\\.com/media/*?format=jpg*",
		proto.NetworkResourceTypeImage,
		func(ctx *rod.Hijack) {
			fb.imageLoadWG.Add(1)
			defer fb.imageLoadWG.Done()

			fb.imageReqFeed <- ctx.Request.URL().String()

			ctx.ContinueRequest(&proto.FetchContinueRequest{})
		},
	)
	go hjRouter.Run()
	fb.hjRouter = hjRouter

	fb.page.
		MustNavigate(fmt.Sprintf("%s/%s/likes", fb.baseUrl, fb.username)).
		MustWaitLoad().
		MustWaitIdle()

	failed := make(chan struct{}, 1)
	loaded := make(chan struct{}, 1)
	_ = fb.page.Race().
		ElementX("/html/body/div[1]/div/div/div[2]/main/div/div/div/div[1]/div/div[3]/div/div/section/div/div").
		Handle(func(e *rod.Element) error {
			loaded <- struct{}{}
			return nil
		}).
		ElementR("span", "Retry").
		Handle(func(e *rod.Element) error {
			failed <- struct{}{}
			return nil
		}).
		MustDo()

	select {
	case <-loaded:
		return scrollFeed
	case <-failed:
		return fb.errorf("failed to load feed")
	}
}

func scrollFeed(fb *feedBrowser) stateFunc {
	if fb.hjRouter == nil {
		panic("nil hijack router")
	}
	defer func() {
		err := fb.hjRouter.Stop()
		if err != nil {
			fb.logger.Errorf("failed to stop hijack router: %s", err)
		}
		fb.hjRouter = nil
	}()

	var retries uint = 0
	for retries < fb.numRetries {
		select {
		case <-fb.ctx.Done():
			return fb.errorf("context canceled: %s", fb.ctx.Err())
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		page := fb.page.Context(ctx)
		err := scrollToLast(page, fb.imageLoadWG)
		if err != nil {
			page.Mouse.Scroll(0, float64(-1*int(retries)*100), 5)
			page.Mouse.Scroll(0, float64(retries*100), 5)

			retries++
			fb.logger.Errorf("scroll to last: %s", err)
			fb.logger.Infof("retrying: %d", retries)
			time.Sleep(time.Duration(retries) * 2 * time.Second)
		} else {
			retries = 0
		}
		cancel()
	}

	return nil
}

func scrollToLast(page *rod.Page, imageLoadWg *sync.WaitGroup) error {
	postEls, err := page.ElementsX("/html/body/div[1]/div/div/div[2]/main/div/div/div/div[1]/div/div[3]/div/div/section/div/div/child::*")
	if err != nil {
		return fmt.Errorf("failed to get post elements: %s", err)
	}

	if postEls.Empty() {
		return fmt.Errorf("found no post elements")
	}

	lastEl := postEls.Last()
	err = lastEl.ScrollIntoView()
	if err != nil {
		return fmt.Errorf("failed to scroll into view of last post element: %s", err)
	}

	imageLoadWg.Wait()

	_, err = lastEl.Element("div[data-testid=tweetText]>span")
	if err != nil {
		return fmt.Errorf("found no last element: %s", err)
	}

	return nil
}
