package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"

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
}

func (ip *imgProcessor) run(imageFeed <-chan string) {
	feed := make(chan string, ip.numWorkers)
	defer func() {
		close(feed)
	}()

	for range ip.numWorkers {
		go ip.processImage(feed)
	}

	for imgUrl := range imageFeed {
		feed <- imgUrl
	}
}

func (ip *imgProcessor) processImage(feed <-chan string) {
	for imgUrl := range feed {
		ctx := context.TODO()

		if c := ip.counter.Add(1); c%100 == 0 {
			ip.logger.Infof("%dth image from feed: %s", c, imgUrl)
		}

		URL, imgID, err := parseImgUrl(imgUrl)
		if err != nil {
			err := ip.imgJobStore.MarkAsFailed(ctx, imgID, imgUrl, fmt.Errorf("parse url: %w", err))
			if err != nil {
				ip.logger.Error("mark as failed", "err", err)
			}
			continue
		}
		logger := ip.logger.With("url", URL.String(), "id", imgID)

		exists, err := ip.imgJobStore.HasDownloaded(ctx, imgID)
		if err != nil {
			logger.Error("failed to check exists", "err", err)
		}
		if exists {
			logger.Debug("duplicate image")
			continue
		}

		err = ip.imgStore.StoreImage(ctx, URL, imgID)
		if err != nil {
			err := ip.imgJobStore.MarkAsFailed(ctx, imgID, imgUrl, fmt.Errorf("download failed: %s", err))
			if err != nil {
				logger.Error("mark as failed", "err", err)
			}
			continue
		}

		err = ip.imgJobStore.MarkAsDownloaded(ctx, imgID, URL)
		if err != nil {
			logger.Error("mark as downloaded", "err", err)
		}
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
