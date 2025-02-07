package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type SqliteImageJobStore struct {
	db *sqlx.DB
}

var dbInitQuery string = `
CREATE TABLE IF NOT EXISTS images (
	id TEXT PRIMARY KEY,
	src_url TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS failed (
	src_url TEXT NOT NULL,
	id TEXT NOT NULL,
	err_msg TEXT NOT NULL
);
`

func NewSqliteImageJobStore(ctx context.Context, dbFile string) (*SqliteImageJobStore, error) {
	db, err := sqlx.ConnectContext(ctx, "sqlite3", dbFile)
	if err != nil {
		return nil, err
	}

	_, err = db.ExecContext(ctx, dbInitQuery)
	if err != nil {
		return nil, err
	}

	return &SqliteImageJobStore{
		db: db,
	}, nil
}

func (s *SqliteImageJobStore) Close() error {
	return s.db.Close()
}

func (s *SqliteImageJobStore) MarkAsDownloaded(ctx context.Context, imageID string, u *url.URL) error {
	query := `
	INSERT INTO images (id, src_url)
	VALUES (?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, imageID, u.String())
	return err
}

func (s *SqliteImageJobStore) HasDownloaded(ctx context.Context, id string) (bool, error) {
	query := `
	SELECT * FROM images
	WHERE id = ?
	`
	var img struct {
		Id  string `db:"id"`
		Src string `db:"src_url"`
	}
	err := s.db.GetContext(ctx, &img, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (s *SqliteImageJobStore) MarkAsFailed(ctx context.Context, imageID string, uri string, reason error) error {
	query := `
	INSERT INTO failed (src_url, id, err_msg)
	VALUES (?, ?, ?)
	`
	if imageID == "" {
		imageID = "unknown"
	}
	_, err := s.db.ExecContext(ctx, query, uri, imageID, reason.Error())
	return err
}
