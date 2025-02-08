package cmd

import (
	"context"
	"errors"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tifye/x-feed-scraper/browser"
	"github.com/tifye/x-feed-scraper/cmd/cli"
	"github.com/tifye/x-feed-scraper/storage"
)

func newRunCommand(cli *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use: "run",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd.Context(), cli.Logger, cli.Config)
		},
	}

	return cmd
}

func runRun(
	ctx context.Context,
	logger *log.Logger,
	config *viper.Viper,
) error {
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
		logger.Info("closed sqlite job store")
	}()

	ln := launcher.NewUserMode().
		Leakless(false).
		Headless(false)
	debugUrl := ln.MustLaunch()
	rodBrowser := rod.New().
		ControlURL(debugUrl).
		MustConnect().
		Context(ctx)
	wg.Add(1)
	defer func() {
		defer wg.Done()
		if err := rodBrowser.Close(); err != nil {
			if !errors.Is(err, context.Canceled) {
				logger.Errorf("failed to close browser: %s", err)
			}
		} else {
			logger.Info("closed rod browser")
		}
	}()

	fb := browser.NewXFeedBrowser(
		logger.WithPrefix("browser"),
		rodBrowser,
		browser.Credentials{
			Username: config.GetString("X_USERNAME"),
			Password: config.GetString("X_PASSWORD"),
		},
	).WithStateChangedHook(func(bs browser.BrowserState) {
		logger.Info("State changed", "state", bs)
	})

	imgProc := storage.NewImgProcessor(logger.WithPrefix("processor"), 5, imgStore, imgJobStore)
	wg.Add(1)
	go func() {
		defer wg.Done()
		imgProc.Run(ctx, fb.ImageRequestFeed())
		logger.Info("image processor completed")
	}()

	fb.Run(ctx)
	return nil
}
