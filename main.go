package main

import (
	"context"
	"os"

	"github.com/charmbracelet/log"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/joho/godotenv"
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
		// SlowMotion(time.Second).
		// Trace(true).
		ControlURL(debugUrl).
		MustConnect()

	logger := log.NewWithOptions(os.Stdout, log.Options{
		ReportCaller:    true,
		ReportTimestamp: false,
		Level:           log.DebugLevel,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fb := &feedBrowser{
		baseUrl:      "https://x.com",
		username:     os.Getenv("X_USERNAME"),
		password:     os.Getenv("X_PASSWORD"),
		numRetries:   10,
		logger:       logger,
		broswer:      browser,
		imageReqFeed: make(chan string, 1),
	}

	imgProc := &imgProcessor{
		logger:     logger.WithPrefix("img-processor"),
		cancelFunc: cancel,
		numWorkers: 5,
		db:         db,
	}
	go imgProc.run(fb.imageReqFeed)

	fb.run(ctx)
}
