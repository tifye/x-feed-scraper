package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type store struct {
	db *sqlx.DB
}

var dbInitQuery string = `
CREATE TABLE IF NOT EXISTS images (
	id TEXT PRIMARY KEY,
	src_url TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS checkpoints (
	time TEXT NOT NULL,
	num_downloaded INTEGER NOT NULL,
	total_downloaded INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS failed (
	src_url TEXT NOT NULL,
	err_msg TEXT NOT NULL
);
`

func newStore(ctx context.Context) (*store, error) {
	db, err := sqlx.ConnectContext(ctx, "sqlite3", "./state.db")
	if err != nil {
		return nil, err
	}

	_, err = db.ExecContext(ctx, dbInitQuery)
	if err != nil {
		return nil, err
	}

	return &store{
		db: db,
	}, nil
}

type storeImage struct {
	Id  string `db:"id"`
	Src string `db:"src_url"`
}

func (s *store) insertImage(ctx context.Context, image storeImage) error {
	query := `
	INSERT INTO images (id, src_url)
	VALUES (?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, image.Id, image.Src)
	return err
}

func (s *store) imageExists(ctx context.Context, id string) (bool, error) {
	query := `
	SELECT * FROM images
	WHERE id = ?
	`
	var img storeImage
	err := s.db.GetContext(ctx, &img, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

type storeFailedImage struct {
	Src string `db:"src_url"`
	Err string `db:"err_msg"`
}

func (s *store) insertFailedImage(ctx context.Context, image storeFailedImage) error {
	query := `
	INSERT INTO failed (src_url, err_msg)
	VALUES (?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, image.Src, image.Err)
	return err
}

type storeCheckpoint struct {
	Time            time.Time `db:"time"`
	NumDownloaded   int       `db:"num_downloaded"`
	TotalDownloaded int       `db:"total_downloaded"`
}

func (s *store) insertCheckpoint(ctx context.Context, checkpoint storeCheckpoint) error {
	query := `
	INSERT INTO checkpoints (time, num_downloaded, total_downloaded)
	VALUES (?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, checkpoint.Time, checkpoint.NumDownloaded, checkpoint.TotalDownloaded)
	return err
}
