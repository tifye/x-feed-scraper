package cli

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/spf13/viper"
)

type CLI struct {
	Logger *log.Logger
	Config *viper.Viper
}

type ImageStoreTypeFlag string

const (
	FileImageStore ImageStoreTypeFlag = "file"
	S3ImageStore   ImageStoreTypeFlag = "s3"
)

func (t *ImageStoreTypeFlag) String() string {
	return string(*t)
}

func (t *ImageStoreTypeFlag) Set(v string) error {
	switch v {
	case "file", "s3":
		*t = ImageStoreTypeFlag(v)
		return nil
	default:
		return fmt.Errorf("must be one of %v", []ImageStoreTypeFlag{FileImageStore, S3ImageStore})
	}
}

func (t *ImageStoreTypeFlag) Type() string {
	return "ImageStoreTypeFlag"
}
