package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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

	var totalAdded, totalRemoved int

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

		remoteIDs := make(map[string]soundcloud.Track, len(playlistData.Entries))
		for _, t := range playlistData.Entries {
			remoteIDs[t.ID] = t
		}

		// Phase 1: remove orphaned tracks
		localTracks, err := database.GetPlaylistTracks(pl.ID)
		if err != nil {
			log.Printf("get local tracks: %v", err)
		}

		unassigned, err := database.GetUnassignedTracks()
		if err != nil {
			log.Printf("get unassigned tracks: %v", err)
		}
		for _, u := range unassigned {
			uPath := filepath.Join(playlistDir, u.Title+ext)
			if _, err := os.Stat(uPath); err == nil {
				_ = database.UpdatePlaylistID(u.ID, pl.ID)
				localTracks = append(localTracks, u)
			}
		}

		for _, lt := range localTracks {
			rt, inRemote := remoteIDs[lt.ID]
			if !inRemote {
				trackPath := filepath.Join(playlistDir, lt.Title+ext)
				if err := os.Remove(trackPath); err != nil && !os.IsNotExist(err) {
					log.Printf("remove %q: %v", trackPath, err)
				}
				if err := database.Delete(lt.ID); err != nil {
					log.Printf("delete db %q: %v", lt.ID, err)
				}
				fmt.Printf("🗑 %s\n", lt.Title)
				totalRemoved++
				continue
			}

			if lt.Title != rt.Title {
				oldPath := filepath.Join(playlistDir, lt.Title+ext)
				if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
					log.Printf("remove old %q: %v", oldPath, err)
				}
				_ = database.UpdateTrack(lt.ID, rt.Title, rt.URL)
				_ = database.MarkNotDownloaded(lt.ID)
			}
		}

		if len(playlistData.Entries) == 0 {
			continue
		}

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
				totalAdded++
			}
		}
	}

	parts := []string{}
	if totalAdded > 0 {
		parts = append(parts, fmt.Sprintf("+%d", totalAdded))
	}
	if totalRemoved > 0 {
		parts = append(parts, fmt.Sprintf("-%d", totalRemoved))
	}
	if len(parts) > 0 {
		log.Printf("done: %s", strings.Join(parts, ", "))
	} else {
		log.Printf("done: nothing changed")
	}

	if err := playlist.Generate(cfg.MusicDir, ext); err != nil {
		log.Printf("m3u: %v", err)
	}
}
