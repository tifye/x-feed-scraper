package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
)

func FileImageStore(dir string) (func(ctx context.Context, u *url.URL, imageID string) error, error) {
	if err := os.MkdirAll(dir, 0644); err != nil {
		return nil, err
	}

	return func(ctx context.Context, u *url.URL, imageID string) error {
		query := u.Query()
		query.Del("name")
		uClone := new(url.URL)
		*uClone = *u
		uClone.RawQuery = query.Encode()

		format := uClone.Query().Get("format")
		if format == "" {
			format = "jpg"
		}
		fpath := path.Join(dir, imageID+"."+format)

		return downloadToFile(ctx, uClone, fpath)
	}, nil
}

func downloadToFile(ctx context.Context, u *url.URL, file string) error {
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return download(ctx, u, f)
}

func download(ctx context.Context, url *url.URL, w io.Writer) error {
	if url == nil {
		panic("calling download on nil URL")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode > 299 {
		return fmt.Errorf("non success status: %d", res.StatusCode)
	}

	_, err = io.Copy(w, res.Body)
	if err != nil {
		return err
	}

	return nil
}
