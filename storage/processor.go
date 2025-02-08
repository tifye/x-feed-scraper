package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/tifye/x-feed-scraper/browser"
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

type ImgProcessor struct {
	logger      *log.Logger
	numWorkers  uint
	counter     atomic.Uint32
	imgStore    ImageStorer
	imgJobStore ImageJobStorer
	workerWG    sync.WaitGroup
}

func NewImgProcessor(
	logger *log.Logger,
	numWorkers uint,
	imgStore ImageStorer,
	imgJobStore ImageJobStorer,
) *ImgProcessor {
	return &ImgProcessor{
		logger:      logger.WithPrefix("processor"),
		numWorkers:  5,
		imgStore:    imgStore,
		imgJobStore: imgJobStore,
	}
}

func (ip *ImgProcessor) Run(ctx context.Context, imageFeed <-chan browser.ImageRequest) {
	feed := make(chan string, ip.numWorkers)
	ip.workerWG.Add(int(ip.numWorkers))
	for i := range ip.numWorkers {
		go func() {
			defer ip.workerWG.Done()
			ip.consumeImages(ctx, feed)
			ip.logger.Debugf("worker %d done", i)
		}()
	}

	for imgRequest := range imageFeed {
		feed <- imgRequest.URL.String()
	}

	close(feed)
	ip.workerWG.Wait()
}

func (ip *ImgProcessor) processImage(ctx context.Context, logger *log.Logger, u *url.URL, imgID string) error {
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

func (ip *ImgProcessor) consumeImages(ctx context.Context, feed <-chan string) {
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
