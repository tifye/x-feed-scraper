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

type imgProcessor struct {
	logger      *log.Logger
	cancelFunc  context.CancelFunc
	numWorkers  int
	db          *store
	counter     atomic.Int32
	imageStorer ImageStorer
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
		if c := ip.counter.Add(1); c%100 == 0 {
			ip.logger.Infof("%dth image from feed: %s", c, imgUrl)
		}

		URL, imgID, err := parseImgUrl(imgUrl)
		if err != nil {
			err := ip.db.insertFailedImage(context.TODO(), storeFailedImage{
				Src: URL.String(),
				Err: fmt.Sprintf("parse url: %s", err),
			})
			if err != nil {
				ip.logger.Errorf("store failed image: %s", err)
			}
			continue
		}

		exists, err := ip.db.imageExists(context.TODO(), imgID)
		if err != nil {
			ip.logger.Error("failed to check exists: %s", err)
		}
		if exists {
			ip.logger.Info("duplicate image", "id", imgID)
			continue
		}

		err = ip.imageStorer.StoreImage(context.TODO(), URL, imgID)
		if err != nil {
			err := ip.db.insertFailedImage(context.TODO(), storeFailedImage{
				Src: URL.String(),
				Err: fmt.Sprintf("download failed: %s", err),
			})
			if err != nil {
				ip.logger.Errorf("store failed image: %s", err)
			}
			continue
		}

		err = ip.db.insertImage(context.TODO(), storeImage{
			Id:  imgID,
			Src: URL.String(),
		})
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
