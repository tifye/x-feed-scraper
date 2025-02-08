package cmd

import (
	"context"
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tifye/x-feed-scraper/cmd/cli"
)

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "feed-me",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	return cmd
}

func Execute(ctx context.Context) {
	config := viper.New()
	config.AutomaticEnv()

	root := newRootCommand()

	logger := log.NewWithOptions(os.Stdout, log.Options{
		ReportCaller:    true,
		ReportTimestamp: false,
		Level:           log.DebugLevel,
	})
	cli := &cli.CLI{
		Logger: logger,
		Config: config,
	}

	addCommands(root, cli)

	if err := root.ExecuteContext(ctx); err != nil {
		logger.Error(err)
	}
}

func addCommands(cmd *cobra.Command, cli *cli.CLI) {
	cmd.AddCommand(newRunCommand(cli))
}
