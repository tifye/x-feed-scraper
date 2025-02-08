package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/charmbracelet/log"
	"github.com/joho/godotenv"
	"github.com/tifye/x-feed-scraper/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	cmd.Execute(ctx)
}
