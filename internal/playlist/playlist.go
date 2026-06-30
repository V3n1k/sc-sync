package playlist

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Generate(musicDir, ext string) error {
	entries, err := os.ReadDir(musicDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read music dir: %w", err)
	}

	var allTracks []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		plDir := filepath.Join(musicDir, entry.Name())
		tracks, err := scanDir(plDir, ext)
		if err != nil {
			continue
		}
		if len(tracks) == 0 {
			continue
		}

		m3uPath := filepath.Join(musicDir, entry.Name()+".m3u")
		if err := writeM3U(m3uPath, tracks); err != nil {
			return fmt.Errorf("write %s: %w", m3uPath, err)
		}

		allTracks = append(allTracks, tracks...)
	}

	allPath := filepath.Join(musicDir, "all.m3u")
	if len(allTracks) > 0 {
		sort.Strings(allTracks)
		if err := writeM3U(allPath, allTracks); err != nil {
			return fmt.Errorf("write all: %w", err)
		}
	} else {
		os.Remove(allPath)
	}

	return nil
}

func scanDir(dir, ext string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var tracks []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		tracks = append(tracks, filepath.Join(dir, e.Name()))
	}
	sort.Strings(tracks)
	return tracks, nil
}

func writeM3U(path string, tracks []string) error {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, t := range tracks {
		name := strings.TrimSuffix(filepath.Base(t), filepath.Ext(t))
		b.WriteString(fmt.Sprintf("#EXTINF:-1,%s\n", name))
		b.WriteString(t + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}
