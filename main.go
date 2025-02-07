package main

import (
	"context"
	"os"

	"github.com/charmbracelet/log"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/joho/godotenv"
	"github.com/tifye/x-feed-scraper/storage"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	db, err := newStore(context.TODO())
	if err != nil {
		log.Fatal(err)
	}

	ln := launcher.NewUserMode().
		Leakless(false).
		Headless(false)
	debugUrl := ln.MustLaunch()
	browser := rod.New().
		ControlURL(debugUrl).
		MustConnect()

	logger := log.NewWithOptions(os.Stdout, log.Options{
		ReportCaller:    true,
		ReportTimestamp: false,
		Level:           log.DebugLevel,
	})

	imgStore, err := storage.FileImageStore("./test2")
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fb := newFeedBrowser(
		logger,
		browser,
		make(chan string, 1),
		10,
		os.Getenv("X_USERNAME"),
		os.Getenv("X_PASSWORD"),
	)

	imgProc := &imgProcessor{
		logger:      logger.WithPrefix("img-processor"),
		cancelFunc:  cancel,
		numWorkers:  5,
		db:          db,
		imageStorer: ImageStorerFunc(imgStore),
	}
	go imgProc.run(fb.imageReqFeed)

	fb.run(ctx)
}
