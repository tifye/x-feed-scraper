package cli

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/viper"
)

type CLI struct {
	Logger *log.Logger
	Config *viper.Viper
}
