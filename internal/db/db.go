package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func Init(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	_, err = conn.Exec(`
		CREATE TABLE IF NOT EXISTS tracks (
			id TEXT PRIMARY KEY,
			playlist_id TEXT DEFAULT '',
			title TEXT,
			url TEXT,
			downloaded INTEGER DEFAULT 0
		);
	`)
	if err != nil {
		return nil, err
	}

	// migrate: add playlist_id if missing
	_, _ = conn.Exec(`ALTER TABLE tracks ADD COLUMN playlist_id TEXT DEFAULT ''`)

	return &DB{conn: conn}, nil
}

func (db *DB) Exists(id string) bool {
	var count int
	db.conn.QueryRow(`SELECT COUNT(*) FROM tracks WHERE id = ? AND downloaded = 1`, id).Scan(&count)
	return count > 0
}

func (db *DB) Save(id, playlistID, title, url string) error {
	_, err := db.conn.Exec(`
		INSERT OR IGNORE INTO tracks(id, playlist_id, title, url, downloaded)
		VALUES (?, ?, ?, ?, 0)
	`, id, playlistID, title, url)
	return err
}

func (db *DB) MarkDownloaded(id string) error {
	_, err := db.conn.Exec(`UPDATE tracks SET downloaded = 1 WHERE id = ?`, id)
	return err
}

func (db *DB) MarkNotDownloaded(id string) error {
	_, err := db.conn.Exec(`UPDATE tracks SET downloaded = 0 WHERE id = ?`, id)
	return err
}

func (db *DB) Delete(id string) error {
	_, err := db.conn.Exec(`DELETE FROM tracks WHERE id = ?`, id)
	return err
}

func (db *DB) GetDownloadedTracks() ([]struct {
	ID         string
	Title      string
	PlaylistID string
}, error) {
	rows, err := db.conn.Query(`SELECT id, title, playlist_id FROM tracks WHERE downloaded = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []struct {
		ID         string
		Title      string
		PlaylistID string
	}
	for rows.Next() {
		var t struct {
			ID         string
			Title      string
			PlaylistID string
		}
		if err := rows.Scan(&t.ID, &t.Title, &t.PlaylistID); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func (db *DB) Prune(musicDir, audioExt string) (int, error) {
	tracks, err := db.GetDownloadedTracks()
	if err != nil {
		return 0, fmt.Errorf("prune: get tracks: %w", err)
	}

	removed := 0
	for _, t := range tracks {
		playlistDir := filepath.Join(musicDir, t.PlaylistID)
		trackPath := filepath.Join(playlistDir, t.Title+audioExt)

		if _, err := os.Stat(trackPath); os.IsNotExist(err) {
			if err := db.Delete(t.ID); err != nil {
				return removed, fmt.Errorf("prune: delete %q: %w", t.ID, err)
			}
			removed++
		}
	}

	return removed, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}
