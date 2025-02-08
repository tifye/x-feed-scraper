package browser

import "net/url"

type ImageFormat string

const (
	PNG  ImageFormat = "png"
	JPEG ImageFormat = "jpeg"
	WEBP ImageFormat = "webp"
	GIF  ImageFormat = "gif"
)

type ImageRequest struct {
	URL      *url.URL
	Format   ImageFormat
	Metadata map[string]string
}

type Credentials struct {
	Username string
	Password string
}

type BrowserState = string
