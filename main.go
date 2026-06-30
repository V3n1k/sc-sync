package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/V3n1k/sc-sync/internal/config"
	"github.com/V3n1k/sc-sync/internal/db"
	"github.com/V3n1k/sc-sync/internal/playlist"
	"github.com/V3n1k/sc-sync/internal/service"
	"github.com/V3n1k/sc-sync/internal/soundcloud"
	"github.com/V3n1k/sc-sync/internal/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if len(os.Args) < 2 {
		runTUI(cfg)
		return
	}

	switch os.Args[1] {
	case "sync":
		headless := false
		for _, a := range os.Args[2:] {
			if a == "--headless" {
				headless = true
			}
		}
		if headless {
			runHeadless(cfg)
		} else {
			runTUI(cfg)
		}
	case "config":
		runTUI(cfg)
	case "service":
		if len(os.Args) < 3 {
			fmt.Println("usage: sc-sync service [create|remove]")
			return
		}
		switch os.Args[2] {
		case "create":
			if err := service.Create(); err != nil {
				log.Fatal(err)
			}
			fmt.Println("✓ service created")
		case "remove":
			if err := service.Remove(); err != nil {
				log.Fatal(err)
			}
			fmt.Println("✓ service removed")
		default:
			fmt.Println("usage: sc-sync service [create|remove]")
		}
	default:
		fmt.Printf("usage: sc-sync [sync|config|service]\n")
	}
}

func runTUI(cfg *config.Config) {
	m := tui.InitialModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	tui.SetProgram(p)

	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func runHeadless(cfg *config.Config) {
	database, err := db.Init(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	ext := "." + cfg.AudioFormat
	if n, err := database.Prune(cfg.MusicDir, ext); err != nil {
		log.Printf("prune: %v", err)
	} else if n > 0 {
		log.Printf("pruned %d missing tracks", n)
	}

	for _, pl := range cfg.Playlists {
		if !pl.Enabled {
			continue
		}
		log.Printf("syncing %s (%s)...", pl.Name, pl.ID)

		playlistURL := cfg.PlaylistURL(pl)
		playlistData, err := soundcloud.GetLikes(playlistURL)
		if err != nil {
			log.Printf("error fetching %s: %v", pl.ID, err)
			continue
		}

		playlistDir := cfg.PlaylistDir(pl)
		for _, t := range playlistData.Entries {
			if database.Exists(t.ID) {
				continue
			}

			name := t.Title
			if name == "" && t.URL != "" {
				parts := strings.Split(strings.TrimRight(t.URL, "/"), "/")
				name = parts[len(parts)-1]
			}
			if name == "" {
				name = t.ID
			}
			fmt.Printf("⬇ %s\n", name)
			_ = database.Save(t.ID, pl.ID, t.Title, t.URL)

			if err := soundcloud.DownloadTrack(t.URL, playlistDir, cfg.AudioFormat); err != nil {
				fmt.Printf("✗ %s: %v\n", name, err)
				_ = database.MarkNotDownloaded(t.ID)
			} else {
				_ = database.MarkDownloaded(t.ID)
				fmt.Printf("✓ %s\n", name)
			}
		}
	}

	if err := playlist.Generate(cfg.MusicDir, ext); err != nil {
		log.Printf("m3u: %v", err)
	}
}
