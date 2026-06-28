package main

import (
	"fmt"
	"log"

	"github.com/v3n1k/sc-sync/internal/soundcloud"
)

func main() {

	likesURL := "https://soundcloud.com/v3n1k-101929212/likes"
	musicDir := "/home/v3n1k/Music/SoundCloud"

	fmt.Println("🔄 получаем лайки...")

	playlist, err := soundcloud.GetLikes(likesURL)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🎵 треков найдено: %d\n\n", len(playlist.Entries))

	for _, t := range playlist.Entries {

		fmt.Println("→", t.Title)

		// если уже есть — пропускаем
		if soundcloud.FileExists(musicDir, t.Title) {
			fmt.Println("   ✓ уже есть")
			continue
		}

		// скачиваем
		err := soundcloud.DownloadTrack(t.URL, musicDir)
		if err != nil {
			fmt.Println("   ✗ ошибка:", err)
		}
	}
}
