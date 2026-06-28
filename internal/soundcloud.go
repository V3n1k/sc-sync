package soundcloud

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Track — один трек
type Track struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Playlist — список треков
type Playlist struct {
	Entries []Track `json:"entries"`
}

// GetLikes получает лайки через yt-dlp
func GetLikes(url string) (Playlist, error) {

	cmd := exec.Command(
		"yt-dlp",
		"--no-update",
		"--flat-playlist",
		"--dump-single-json",
		url,
	)

	output, err := cmd.Output()
	if err != nil {
		return Playlist{}, err
	}

	var playlist Playlist
	err = json.Unmarshal(output, &playlist)
	if err != nil {
		return Playlist{}, err
	}

	return playlist, nil
}

// DownloadTrack скачивает трек в папку
func DownloadTrack(url string, dir string) error {

	// создаём папку если нет
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	fmt.Println("⬇ скачиваю:", url)

	cmd := exec.Command(
		"yt-dlp",
		"-x", // извлечь аудио
		"--audio-format", "opus",
		"-P", dir, // папка назначения
		url,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// FileExists проверяет есть ли уже файл
func FileExists(dir, title string) bool {

	safeName := filepath.Join(dir, title+".opus")

	_, err := os.Stat(safeName)
	return err == nil
}
