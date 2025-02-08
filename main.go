package main

import (
	"context"
	"os"
	"os/signal"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/joho/godotenv"
	"github.com/tifye/x-feed-scraper/browser"
	"github.com/tifye/x-feed-scraper/storage"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	logger := log.NewWithOptions(os.Stdout, log.Options{
		ReportCaller:    true,
		ReportTimestamp: false,
		Level:           log.DebugLevel,
	})

	wg := sync.WaitGroup{}
	defer wg.Wait()

	// imgStore, err := storage.FileImageStore("./test")
	// if err != nil {
	// 	logger.Fatal(err)
	// }
	imgStore, err := storage.NewS3Store(ctx, logger.WithPrefix("s3-store"))
	if err != nil {
		logger.Fatal(err)
	}

	imgJobStore, err := storage.NewSqliteImageJobStore(ctx, "./state.db")
	if err != nil {
		logger.Fatal(err)
	}
	wg.Add(1)
	defer func() {
		defer wg.Done()
		err := imgJobStore.Close()
		if err != nil {
			logger.Error(err)
		}
		logger.Debug("closed sqlite job store")
	}()

	ln := launcher.NewUserMode().
		Leakless(false).
		Headless(true)
	debugUrl := ln.MustLaunch()
	rodBrowser := rod.New().
		ControlURL(debugUrl).
		MustConnect().
		Context(ctx)

	fb := browser.NewFeedBrowser(
		logger.WithPrefix("browser"),
		rodBrowser,
		10,
		browser.Credentials{
			Username: os.Getenv("X_USERNAME"),
			Password: os.Getenv("X_PASSWORD"),
		},
	)

	imgProc := &imgProcessor{
		logger:      logger.WithPrefix("processor"),
		cancelFunc:  cancel,
		numWorkers:  5,
		imgStore:    imgStore,
		imgJobStore: imgJobStore,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		imgProc.run(ctx, fb.ImageRequestFeed())
	}()

	fb.Run(ctx)
}
