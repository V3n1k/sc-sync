package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/V3n1k/sc-sync/internal/config"
	"github.com/V3n1k/sc-sync/internal/db"
	"github.com/V3n1k/sc-sync/internal/soundcloud"
)

type TrackUpdate struct {
	PlaylistID string
	Title      string
	URL        string
	Progress   float64
	Status     Status
	Err        string
	Total      int
	Current    int
}

type Status int

const (
	Downloading Status = iota
	Done
	Error
	Skipped
	Phase
)

type StatusMsg struct {
	PlaylistID string
	Message    string
}

type Syncer struct {
	cfg    *config.Config
	db     *db.DB
	mu     sync.Mutex
	paused bool
	pauseCh chan struct{}
	stats  SyncStats
}

type SyncStats struct {
	mu       sync.Mutex
	total    int
	done     int
	skipped  int
	errors   int
}

func (s *SyncStats) snapshot() (total, done, skipped, errors int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total, s.done, s.skipped, s.errors
}

func New(cfg *config.Config, database *db.DB) *Syncer {
	return &Syncer{
		cfg:     cfg,
		db:      database,
		pauseCh: make(chan struct{}, 1),
	}
}

func (s *Syncer) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
}

func (s *Syncer) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		s.paused = false
		select {
		case s.pauseCh <- struct{}{}:
		default:
		}
	}
}

func (s *Syncer) IsPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

func (s *Syncer) checkPause(ctx context.Context) error {
	s.mu.Lock()
	paused := s.paused
	s.mu.Unlock()

	if paused {
		select {
		case <-s.pauseCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type trackJob struct {
	track soundcloud.Track
	index int
	total int
}

func audioExt(format string) string {
	return "." + format
}

func (s *Syncer) SyncPlaylist(ctx context.Context, playlist config.Playlist, updates chan<- TrackUpdate, statusCh chan<- StatusMsg) error {
	playlistURL := s.cfg.PlaylistURL(playlist)
	playlistDir := s.cfg.PlaylistDir(playlist)

	if err := os.MkdirAll(playlistDir, 0755); err != nil {
		return fmt.Errorf("mkdir %q: %w", playlistDir, err)
	}

	ext := audioExt(s.cfg.AudioFormat)

	if _, err := s.db.Prune(s.cfg.MusicDir, ext); err != nil {
		log.Printf("prune: %v", err)
	}

	statusCh <- StatusMsg{PlaylistID: playlist.ID, Message: "Fetching playlist..."}

	playlistData, err := soundcloud.GetLikes(playlistURL)
	if err != nil {
		return fmt.Errorf("get playlist %q: %w", playlist.ID, err)
	}

	total := len(playlistData.Entries)

	if total == 0 {
		statusCh <- StatusMsg{PlaylistID: playlist.ID, Message: "Playlist is empty, nothing to download"}
		return nil
	}

	statusCh <- StatusMsg{PlaylistID: playlist.ID, Message: fmt.Sprintf("Found %d tracks, checking...", total)}

	concurrency := s.cfg.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	jobs := make(chan trackJob, total)
	var wg sync.WaitGroup

	s.stats = SyncStats{}
	s.stats.total = total

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go s.downloadWorker(ctx, playlist, playlistDir, ext, jobs, updates, statusCh, &wg)
	}

	for i, t := range playlistData.Entries {
		if err := ctx.Err(); err != nil {
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}

		if s.db.Exists(t.ID) {
			s.stats.skipped++
			updates <- TrackUpdate{
				PlaylistID: playlist.ID,
				Title:      t.Title,
				URL:        t.URL,
				Progress:   1.0,
				Status:     Skipped,
				Total:      total,
				Current:    i + 1,
			}
			continue
		}

		jobs <- trackJob{track: t, index: i + 1, total: total}
	}

	close(jobs)
	wg.Wait()

	return nil
}

func (s *Syncer) downloadWorker(ctx context.Context, playlist config.Playlist, dir, ext string, jobs <-chan trackJob, updates chan<- TrackUpdate, statusCh chan<- StatusMsg, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		if err := ctx.Err(); err != nil {
			return
		}

		if err := s.checkPause(ctx); err != nil {
			return
		}

		t := job.track

		updates <- TrackUpdate{
			PlaylistID: playlist.ID,
			Title:      t.Title,
			URL:        t.URL,
			Progress:   0,
			Status:     Downloading,
			Total:      job.total,
			Current:    job.index,
		}

		_ = s.db.Save(t.ID, playlist.ID, t.Title, t.URL)

		err := soundcloud.DownloadTrack(t.URL, dir, s.cfg.AudioFormat)

		s.stats.mu.Lock()
		if err == nil {
			_ = s.db.MarkDownloaded(t.ID)
			s.stats.done++
		} else {
			_ = s.db.MarkNotDownloaded(t.ID)
			s.stats.errors++
		}
		done, skipped, errors := s.stats.done, s.stats.skipped, s.stats.errors
		s.stats.mu.Unlock()

		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		status := Done
		if err != nil {
			status = Error
		}
		updates <- TrackUpdate{
			PlaylistID: playlist.ID,
			Title:      t.Title,
			URL:        t.URL,
			Progress:   1.0,
			Status:     status,
			Err:        errMsg,
			Total:      job.total,
			Current:    job.index,
		}

		statusCh <- StatusMsg{
			PlaylistID: playlist.ID,
			Message:    fmt.Sprintf("Downloading %d/%d tracks (%d done, %d skipped, %d errors)", done+skipped+errors, job.total, done, skipped, errors),
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func (s *Syncer) SyncAll(ctx context.Context, updates chan<- TrackUpdate, statusCh chan<- StatusMsg) error {
	for _, pl := range s.cfg.Playlists {
		if !pl.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.SyncPlaylist(ctx, pl, updates, statusCh); err != nil {
			return err
		}
	}

	return nil
}
