package cmd

import (
	"context"
	"errors"
	"os"
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

type runOptions struct {
	imageStoreType   cli.ImageStoreTypeFlag
	imageStoreDir    string
	dbSource         string
	wipeOnCompletion bool
}

func newRunCommand(c *cli.CLI) *cobra.Command {
	opts := runOptions{}

	cmd := &cobra.Command{
		Use: "run",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd.Context(), c.Logger, c.Config, opts)
		},
	}

	cmd.Flags().Var(&opts.imageStoreType, "store", "How image objects are stored")
	cmd.Flags().StringVar(&opts.imageStoreDir, "dir", "./images", "Where to store image files")
	cmd.Flags().StringVar(&opts.dbSource, "db", "./state.db", "Sqlite DB source")
	cmd.Flags().BoolVar(&opts.wipeOnCompletion, "wipe", false, "Clear metadata DB on completion")

	return cmd
}

func runRun(
	ctx context.Context,
	logger *log.Logger,
	config *viper.Viper,
	opts runOptions,
) error {
	wg := sync.WaitGroup{}
	defer wg.Wait()

	var imgStore storage.ImageStorer
	switch opts.imageStoreType {
	case cli.FileImageStore:
		if opts.imageStoreDir == "" {
			opts.imageStoreDir = "./images"
		}
		is, err := storage.FileImageStore(opts.imageStoreDir)
		if err != nil {
			return err
		}
		imgStore = storage.ImageStorerFunc(is)
	default:
		is, err := storage.NewS3Store(ctx, logger.WithPrefix("s3-store"))
		if err != nil {
			return err
		}
		imgStore = is
	}

	if opts.dbSource == "" {
		opts.dbSource = "./state.db"
	}
	imgJobStore, err := storage.NewSqliteImageJobStore(ctx, opts.dbSource)
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

		if opts.wipeOnCompletion {
			if err := os.Remove(opts.dbSource); err != nil {
				logger.Errorf("failed to wipe DB: %s", err)
			}
		}
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
				return
			}
		}
		logger.Info("closed rod browser")
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
