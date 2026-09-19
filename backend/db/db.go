package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound      = errors.New("url not found")
	ErrAlreadyExists = errors.New("slug already exists")
)

type URLStats struct {
	Slug      string `json:"slug"`
	TargetURL string `json:"targetUrl"`
	Hits      int64  `json:"hits"`
}

type Store struct {
	db *sql.DB
}

func NewStore(dataSourceName string) (*Store, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	schema := `CREATE TABLE IF NOT EXISTS urls (
		slug TEXT PRIMARY KEY,
		target_url TEXT NOT NULL,
		hits INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func generateSlug() (string, error) {
	bytes := make([]byte, 3)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (s *Store) CreateURL(targetURL, customSlug string) (string, error) {
	slug := customSlug
	if slug == "" {
		var err error
		slug, err = generateSlug()
		if err != nil {
			return "", err
		}
	}

	_, err := s.db.Exec("INSERT INTO urls (slug, target_url, hits) VALUES (?, ?, 0)", slug, targetURL)
	if err != nil {
		return "", fmt.Errorf("insert url: %w", err)
	}
	return slug, nil
}

func (s *Store) GetAndIncrementHits(slug string) (string, int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()

	var targetURL string
	var hits int64
	err = tx.QueryRow("SELECT target_url, hits FROM urls WHERE slug = ?", slug).Scan(&targetURL, &hits)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	} else if err != nil {
		return "", 0, err
	}

	hits++
	if _, err := tx.Exec("UPDATE urls SET hits = ? WHERE slug = ?", hits, slug); err != nil {
		return "", 0, err
	}

	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return targetURL, hits, nil
}

func (s *Store) GetStats(slug string) (*URLStats, error) {
	var targetURL string
	var hits int64
	err := s.db.QueryRow("SELECT target_url, hits FROM urls WHERE slug = ?", slug).Scan(&targetURL, &hits)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &URLStats{
		Slug:      slug,
		TargetURL: targetURL,
		Hits:      hits,
	}, nil
}

func (s *Store) DeleteURL(slug string) error {
	res, err := s.db.Exec("DELETE FROM urls WHERE slug = ?", slug)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
