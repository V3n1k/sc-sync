package soundcloud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Track struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Playlist struct {
	Entries []Track `json:"entries"`
}

func GetLikes(url string) (Playlist, error) {
	cmd := exec.Command(
		"yt-dlp",
		"--no-update",
		"--flat-playlist",
		"--dump-single-json",
		url,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		errMsg := ""
		if s := stderr.String(); len(s) > 0 {
			errMsg = s
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
		} else if len(output) > 0 {
			errMsg = string(output)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
		}
		return Playlist{}, fmt.Errorf("%w\n%s", err, errMsg)
	}

	var playlist Playlist
	err = json.Unmarshal(output, &playlist)
	if err != nil {
		return Playlist{}, err
	}

	return playlist, nil
}

func DownloadTrack(url string, dir string, format string) error {
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	outputTemplate := filepath.Join(dir, "%(title)s.%(ext)s")

	cmd := exec.Command(
		"yt-dlp",
		"-x",
		"--audio-format", format,
		"-o", outputTemplate,
		"--no-progress",
		"--embed-thumbnail",
		"--add-metadata",
		url,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if len(errMsg) > 300 {
			errMsg = errMsg[:300] + "..."
		}
		if errMsg != "" {
			return fmt.Errorf("%s", errMsg)
		}
		return err
	}
	return nil
}

func FileExists(dir, title, ext string) bool {
	safeName := filepath.Join(dir, title+ext)
	_, err := os.Stat(safeName)
	return err == nil
}
