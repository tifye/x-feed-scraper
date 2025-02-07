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
	MarkAsFailed(ctx context.Context, imageID string, u *url.URL, reason error) error
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
			// err := ip.db.insertFailedImage(context.TODO(), storeFailedImage{
			// 	Src: URL.String(),
			// 	Err: fmt.Sprintf("parse url: %s", err),
			// })
			err := ip.imgJobStore.MarkAsFailed(ctx, imgID, URL, fmt.Errorf("parse url: %w", err))
			if err != nil {
				ip.logger.Errorf("store failed image: %s", err)
			}
			continue
		}

		// exists, err := ip.db.imageExists(context.TODO(), imgID)
		exists, err := ip.imgJobStore.HasDownloaded(ctx, imgID)
		if err != nil {
			ip.logger.Error("failed to check exists: %s", err)
		}
		if exists {
			ip.logger.Info("duplicate image", "id", imgID)
			continue
		}

		err = ip.imgStore.StoreImage(ctx, URL, imgID)
		if err != nil {
			// err := ip.db.insertFailedImage(context.TODO(), storeFailedImage{
			// 	Src: URL.String(),
			// 	Err: fmt.Sprintf("download failed: %s", err),
			// })
			err := ip.imgJobStore.MarkAsFailed(ctx, imgID, URL, fmt.Errorf("download failed: %s", err))
			if err != nil {
				ip.logger.Errorf("store failed image: %s", err)
			}
			continue
		}

		// err = ip.db.insertImage(context.TODO(), storeImage{
		// 	Id:  imgID,
		// 	Src: URL.String(),
		// })
		err = ip.imgJobStore.MarkAsDownloaded(ctx, imgID, URL)
		if err != nil {
			ip.logger.Errorf("store image: %s", err)
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
