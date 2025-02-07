package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
)

type ImageStorerFunc func(ctx context.Context, u *url.URL, imageID string) error

func (f ImageStorerFunc) StoreImage(ctx context.Context, u *url.URL, imageID string) error {
	return f(ctx, u, imageID)
}

type ImageStorer interface {
	StoreImage(ctx context.Context, u *url.URL, imageID string) error
}

type ImageJobStorer interface {
	HasDownloaded(ctx context.Context, imageID string) (bool, error)
	MarkAsDownloaded(ctx context.Context, imageID string, u *url.URL) error
	MarkAsFailed(ctx context.Context, imageID string, uri string, reason error) error
}

type imgProcessor struct {
	logger      *log.Logger
	cancelFunc  context.CancelFunc
	numWorkers  int
	counter     atomic.Int32
	imgStore    ImageStorer
	imgJobStore ImageJobStorer
	workerWG    sync.WaitGroup
}

func (ip *imgProcessor) run(ctx context.Context, imageFeed <-chan string) {
	feed := make(chan string, ip.numWorkers)
	ip.workerWG.Add(ip.numWorkers)
	for i := range ip.numWorkers {
		go func() {
			defer ip.workerWG.Done()
			ip.consumeImages(ctx, feed)
			ip.logger.Debugf("worker %d done", i)
		}()
	}

	for imgUrl := range imageFeed {
		feed <- imgUrl
	}

	close(feed)
	ip.workerWG.Wait()
}

func (ip *imgProcessor) processImage(ctx context.Context, logger *log.Logger, u *url.URL, imgID string) error {
	exists, err := ip.imgJobStore.HasDownloaded(ctx, imgID)
	if err != nil {
		logger.Error("failed to check exists", "err", err)
	}
	if exists {
		logger.Debug("duplicate image")
		return nil
	}

	err = ip.imgStore.StoreImage(ctx, u, imgID)
	if err != nil {
		err := ip.imgJobStore.MarkAsFailed(ctx, imgID, u.String(), fmt.Errorf("download failed: %s", err))
		if err != nil {
			return fmt.Errorf("mark as failed: %s", err)
		}
		return nil
	}

	err = ip.imgJobStore.MarkAsDownloaded(ctx, imgID, u)
	if err != nil {
		return fmt.Errorf("mark as downloaded: %s", err)
	}

	return nil
}

func (ip *imgProcessor) consumeImages(ctx context.Context, feed <-chan string) {
	for imgUrl := range feed {
		if c := ip.counter.Add(1); c%100 == 0 {
			ip.logger.Infof("%dth image from feed: %s", c, imgUrl)
		}

		URL, imgID, err := parseImgUrl(imgUrl)
		if err != nil {
			err := ip.imgJobStore.MarkAsFailed(ctx, imgID, imgUrl, fmt.Errorf("parse url: %w", err))
			if err != nil {
				ip.logger.Error("mark as failed", "url", imgUrl, "err", err)
			}
			continue
		}
		logger := ip.logger.With("url", URL.String(), "id", imgID)

		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		err = ip.processImage(ctx, logger, URL, imgID)
		if err != nil {
			logger.Error(err)
		}
		cancel()
	}
}

func parseImgUrl(imgUrl string) (*url.URL, string, error) {
	u, err := url.Parse(imgUrl)
	if err != nil {
		return nil, "", err
	}

	parts := strings.Split(u.Path, "/")
	imgId := parts[len(parts)-1]

	return u, imgId, nil
}
