package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/log"
)

type ImageStorerFunc func(ctx context.Context, details ImageURLDetails) error

func (f ImageStorerFunc) StoreImage(ctx context.Context, details ImageURLDetails) error {
	return f(ctx, details)
}

type ImageStorer interface {
	StoreImage(ctx context.Context, details ImageURLDetails) error
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

		details, err := parseImgUrl(imgUrl)
		if err != nil {
			err := ip.db.insertFailedImage(context.TODO(), storeFailedImage{
				Src: imgUrl,
				Err: fmt.Sprintf("parse url: %s", err),
			})
			if err != nil {
				ip.logger.Errorf("store failed image: %s", err)
			}
			continue
		}

		exists, err := ip.db.imageExists(context.TODO(), details.ImageId)
		if err != nil {
			ip.logger.Error("failed to check exists: %s", err)
		}
		if exists {
			ip.logger.Info("duplicate image", "id", details.ImageId)
			continue
		}

		err = ip.imageStorer.StoreImage(context.TODO(), details)
		if err != nil {
			err := ip.db.insertFailedImage(context.TODO(), storeFailedImage{
				Src: imgUrl,
				Err: fmt.Sprintf("download failed: %s", err),
			})
			if err != nil {
				ip.logger.Errorf("store failed image: %s", err)
			}
			continue
		}

		err = ip.db.insertImage(context.TODO(), storeImage{
			Id:  details.ImageId,
			Src: details.Stripped.String(),
		})
		if err != nil {
			ip.logger.Errorf("store image: %s", err)
		}
	}
}

type ImageURLDetails struct {
	Source *url.URL

	// Source stripped from 'name' query
	Stripped *url.URL

	ImageId string
}

func (d ImageURLDetails) Format() string {
	return d.Source.Query().Get("format")
}

func (d ImageURLDetails) NameKey() string {
	return d.Source.Query().Get("name")
}

func parseImgUrl(imgUrl string) (ImageURLDetails, error) {
	u, err := url.Parse(imgUrl)
	if err != nil {
		return ImageURLDetails{}, err
	}

	uClone := new(url.URL)
	*uClone = *u

	query := uClone.Query()
	query.Del("name") // todo: keep or store as tag
	uClone.RawQuery = query.Encode()

	parts := strings.Split(u.Path, "/")
	imgId := parts[len(parts)-1]

	details := ImageURLDetails{
		Source:   u,
		Stripped: uClone,
		ImageId:  imgId,
	}

	return details, nil
}
