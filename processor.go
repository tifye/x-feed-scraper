package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
)

type proccessedEv struct {
	imgId  string
	imgUrl string
	srcUrl string
}

type imgProcessor struct {
	logger     *log.Logger
	cancelFunc context.CancelFunc
	numWorkers int
	db         *store
	counter    atomic.Int32
}

func (ip *imgProcessor) run(imageFeed <-chan string) {
	feed := make(chan string, ip.numWorkers)
	evch := make(chan proccessedEv)
	defer func() {
		close(evch)
		close(feed)
	}()

	go ip.trackDownloads(evch)

	for range ip.numWorkers {
		go ip.processImage(feed, evch)
	}

	for imgUrl := range imageFeed {
		feed <- imgUrl
	}
}

func (ip *imgProcessor) trackDownloads(evch <-chan proccessedEv) {
	ticker := time.NewTicker(5 * time.Minute)
	numDownloaded := 0
	totalDownloaded := 0
	for ev := range evch {
		numDownloaded += 1
		totalDownloaded += 1

		select {
		case <-ticker.C:
			err := ip.db.insertCheckpoint(context.TODO(), storeCheckpoint{
				Time:            time.Now(),
				NumDownloaded:   numDownloaded,
				TotalDownloaded: totalDownloaded,
			})
			if err != nil {
				ip.logger.Errorf("insert checkpoint: %s", err)
			} else {
				ip.logger.Info("checkpoint hit", "imgId", ev.imgId, "since last", numDownloaded, "total", totalDownloaded)
				numDownloaded = 0
			}
		default:
		}
	}
}

func (ip *imgProcessor) processImage(feed <-chan string, evch chan<- proccessedEv) {
	for imgUrl := range feed {
		if c := ip.counter.Add(1); c%100 == 0 {
			ip.logger.Infof("%dth image from feed: %s", c, imgUrl)
		}

		uri, id, err := parseImgUrl(imgUrl)
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

		exists, err := ip.db.imageExists(context.TODO(), id)
		if err != nil {
			ip.logger.Error("failed to check exists: %s", err)
		}
		if exists {
			ip.logger.Info("duplicate image", "id", id)
			continue
		}

		filename := fmt.Sprintf("%s.jpg", id)
		err = downloadImage(context.TODO(), uri, filename)
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
			Id:  id,
			Src: uri,
		})
		if err != nil {
			ip.logger.Errorf("store image: %s", err)
		}

		evch <- proccessedEv{
			srcUrl: imgUrl,
			imgId:  id,
			imgUrl: uri,
		}
	}
}

func parseImgUrl(imgUrl string) (ogUrl string, imgId string, err error) {
	u, err := url.Parse(imgUrl)
	if err != nil {
		return "", "", err
	}

	query := u.Query()
	query.Del("name")
	u.RawQuery = query.Encode()

	parts := strings.Split(u.Path, "/")
	imgId = parts[len(parts)-1]

	return u.String(), imgId, nil
}

func downloadImage(ctx context.Context, imgUrl string, name string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", imgUrl, nil)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode > 299 {
		return fmt.Errorf("non success status: %d", res.StatusCode)
	}

	f, err := os.OpenFile("./test/"+name, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, res.Body)
	if err != nil {
		return err
	}

	return nil
}
