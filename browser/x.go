package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type xFeedStateFunc func(context.Context, *XFeedBrowser) xFeedStateFunc

const (
	_DefaultXFeedBrowserRetries = 10
	_DefaultXBaseURL            = "https://x.com"
)

type XFeedBrowser struct {
	baseUrl          string
	creds            Credentials
	numRetries       uint
	logger           *log.Logger
	broswer          *rod.Browser
	page             *rod.Page
	imageReqFeed     chan ImageRequest
	imageLoadWG      *sync.WaitGroup
	hjRouter         *rod.HijackRouter
	stateChangeHooks []func(BrowserState)
}

func NewXFeedBrowser(
	logger *log.Logger,
	browser *rod.Browser,
	creds Credentials,
) *XFeedBrowser {
	return &XFeedBrowser{
		baseUrl:          _DefaultXBaseURL,
		creds:            creds,
		numRetries:       _DefaultXFeedBrowserRetries,
		logger:           logger,
		broswer:          browser,
		imageReqFeed:     make(chan ImageRequest, 1),
		imageLoadWG:      &sync.WaitGroup{},
		stateChangeHooks: make([]func(BrowserState), 0),
	}
}

func (fb *XFeedBrowser) WithStateChangedHook(f func(BrowserState)) *XFeedBrowser {
	fb.stateChangeHooks = append(fb.stateChangeHooks, f)
	return fb
}

func (fb *XFeedBrowser) ImageRequestFeed() <-chan ImageRequest {
	return fb.imageReqFeed
}

func (fb *XFeedBrowser) Run(ctx context.Context) {
	for state := fb.navigateToRoot; state != nil; {
		state = state(ctx, fb)
	}
}

func (fb *XFeedBrowser) notifyStateChanged(state BrowserState) {
	for _, f := range fb.stateChangeHooks {
		f(state)
	}
}

func (fb *XFeedBrowser) errorf(format string, args ...interface{}) xFeedStateFunc {
	return fb.error(fmt.Errorf(format, args...))
}

func (fb *XFeedBrowser) error(err error) xFeedStateFunc {
	fb.notifyStateChanged("Error")

	fb.logger.Error(err)
	if err := fb.stopHijack(); err != nil {
		fb.logger.Errorf("stop hijack: %s", err)
	}

	return nil
}

func (*XFeedBrowser) navigateToRoot(_ context.Context, fb *XFeedBrowser) xFeedStateFunc {
	fb.notifyStateChanged("Navigating to root")

	var url string

	err := rod.Try(func() {
		page := fb.broswer.
			MustPage(fb.baseUrl).
			MustWaitIdle()
		fb.page = page

		url = page.MustInfo().URL
	})
	if err != nil {
		return fb.errorf("navigate to root: %s", err)
	}

	if strings.Contains(url, "home") {
		return fb.navigateToLikedTweets
	}

	return fb.navigateToLogin
}

func (*XFeedBrowser) navigateToLogin(_ context.Context, fb *XFeedBrowser) xFeedStateFunc {
	fb.notifyStateChanged("Navigating to login")

	err := rod.Try(func() {
		_ = fb.page.MustNavigate(fb.baseUrl + "/i/flow/login").
			MustWaitIdle()

		_ = fb.page.MustElementR("span", "Sign in to X")

	})
	if err != nil {
		return fb.errorf("navigate to login: %s", err)
	}

	return fb.login
}

func (*XFeedBrowser) login(_ context.Context, fb *XFeedBrowser) xFeedStateFunc {
	fb.notifyStateChanged("Logging in")

	page := fb.page

	var url string

	err := rod.Try(func() {
		_ = page.MustElement("input[name=text]").
			MustInput(fb.creds.Username)

		_ = page.MustElement("button.css-175oi2r:nth-child(6)").
			MustClick()

		_ = page.MustElement("input[name=password]").
			MustInput(fb.creds.Password)

		wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)

		_ = page.MustElement(".r-19yznuf").
			MustClick()

		wait()

		url = page.MustWaitIdle().
			MustInfo().URL
	})
	if err != nil {
		return fb.errorf("exec login: %s", err)
	}

	if strings.Contains(url, "home") {
		return fb.navigateToLikedTweets
	} else {
		return fb.errorf("not at home url: %s", url)
	}
}

func (*XFeedBrowser) navigateToLikedTweets(ctx context.Context, fb *XFeedBrowser) xFeedStateFunc {
	fb.notifyStateChanged("Navigating to liked tweets")

	hjRouter := fb.page.HijackRequests()
	err := hjRouter.Add(
		"https://pbs\\.twimg\\.com/media/*?format=jpg*",
		proto.NetworkResourceTypeImage,
		func(hijack *rod.Hijack) {
			fb.imageLoadWG.Add(1)
			defer fb.imageLoadWG.Done()

			u := hijack.Request.URL()
			query := u.Query()
			ogName := query.Get("name")
			query.Set("name", "large")
			u.RawQuery = query.Encode()

			fb.imageReqFeed <- ImageRequest{
				URL:    u,
				Format: JPEG,
				Metadata: map[string]string{
					"name":         "large",
					"originalName": ogName,
				},
			}

			hijack.ContinueRequest(&proto.FetchContinueRequest{})
		},
	)
	if err != nil {
		return fb.errorf("add hijacker: %s", err)
	}
	go hjRouter.Run()
	fb.hjRouter = hjRouter

	failed := make(chan struct{}, 1)
	loaded := make(chan struct{}, 1)
	err = rod.Try(func() {
		fb.page.
			MustNavigate(fmt.Sprintf("%s/%s/likes", fb.baseUrl, fb.creds.Username)).
			MustWaitLoad().
			MustWaitIdle()

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
	})
	if err != nil {
		return fb.errorf("navigate to likes: %s", err)
	}

	select {
	case <-ctx.Done():
		return fb.error(ctx.Err())
	case <-loaded:
		return fb.scrollFeed
	case <-failed:
		return fb.errorf("failed to load feed")
	}
}

func (*XFeedBrowser) scrollFeed(ctx context.Context, fb *XFeedBrowser) xFeedStateFunc {
	fb.notifyStateChanged("Scrolling feed")

	if fb.hjRouter == nil {
		panic("nil hijack router")
	}
	defer func() {
		if err := fb.stopHijack(); err != nil {
			fb.logger.Errorf("stop hijack: %s", err)
		}
	}()

	var retries uint = 0
	for retries < fb.numRetries {
		select {
		case <-ctx.Done():
			return fb.error(ctx.Err())
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		page := fb.page.Context(ctx)
		err := scrollToLast(page, fb.imageLoadWG)
		if err != nil {
			_ = page.Mouse.Scroll(0, float64(-1*int(retries)*100), 5)
			_ = page.Mouse.Scroll(0, float64(retries*100), 5)

			retries++
			if retries > 1 {
				fb.logger.Warnf("scroll to last: %s, retrying (%d)", err, retries)
			}

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

func (fb *XFeedBrowser) stopHijack() error {
	if fb.hjRouter == nil {
		return nil
	}

	err := fb.hjRouter.Stop()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	fb.hjRouter = nil

	fb.imageLoadWG.Wait()
	close(fb.imageReqFeed)
	return nil
}
