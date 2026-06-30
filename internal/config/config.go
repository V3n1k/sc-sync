package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var validFormats = map[string]bool{
	"opus": true,
	"mp3":  true,
	"m4a":  true,
	"flac": true,
	"wav":  true,
}

type Playlist struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URLPath string `json:"url_path"`
	Enabled bool   `json:"enabled"`
}

type Config struct {
	Username    string     `json:"username"`
	MusicDir    string     `json:"music_dir"`
	DBPath      string     `json:"db_path"`
	AudioFormat string     `json:"audio_format"`
	Concurrency int        `json:"concurrency"`
	Playlists   []Playlist `json:"playlists"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Username:    "v3n1k-101929212",
		MusicDir:    filepath.Join(home, "Music", "SoundCloud"),
		DBPath:      filepath.Join(home, ".local", "share", "sc-sync", "state.db"),
		AudioFormat: "mp3",
		Concurrency: 3,
		Playlists: []Playlist{
			{
				ID:      "likes",
				Name:    "Лайки",
				URLPath: "likes",
				Enabled: true,
			},
		},
	}
}

func (c *Config) PlaylistURL(p Playlist) string {
	if strings.Contains(p.URLPath, "://") {
		return p.URLPath
	}
	return fmt.Sprintf("https://soundcloud.com/%s/%s", c.Username, p.URLPath)
}

func (c *Config) PlaylistDir(p Playlist) string {
	return filepath.Join(c.MusicDir, p.ID)
}

func (c *Config) Validate() error {
	if c.Username == "" {
		return fmt.Errorf("username is required")
	}
	if c.MusicDir == "" {
		return fmt.Errorf("music_dir is required")
	}
	if c.DBPath == "" {
		return fmt.Errorf("db_path is required")
	}
	if !validFormats[c.AudioFormat] {
		return fmt.Errorf("invalid audio_format %q, valid: opus, mp3, m4a, flac, wav", c.AudioFormat)
	}
	for i, p := range c.Playlists {
		if p.ID == "" {
			return fmt.Errorf("playlist %d: id is required", i)
		}
		if p.URLPath == "" {
			return fmt.Errorf("playlist %q: url_path is required", p.ID)
		}
	}
	return nil
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "sc-sync")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func (c *Config) Normalize() {
	for i := range c.Playlists {
		up := c.Playlists[i].URLPath
		if strings.Contains(up, "://") {
			continue
		}
		if !strings.Contains(up, "/") {
			c.Playlists[i].URLPath = "sets/" + up
		}
	}
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			if err := cfg.Save(); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}

	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cfg.Normalize()
	return cfg, nil
}

func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
