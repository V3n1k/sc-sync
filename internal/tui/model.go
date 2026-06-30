package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/V3n1k/sc-sync/internal/config"
	"github.com/V3n1k/sc-sync/internal/db"
	"github.com/V3n1k/sc-sync/internal/playlist"
	"github.com/V3n1k/sc-sync/internal/service"
	"github.com/V3n1k/sc-sync/internal/sync"
)

type page int

const (
	homePage page = iota
	syncPage
	addPage
	deletePage
	settingsPage
)

type syncTrack struct {
	title    string
	url      string
	status   sync.Status
	progress float64
	err      string
}

type model struct {
	cfg  *config.Config
	page page

	// home
	cursor int

	// sync
	syncDone      bool
	syncTracks    []syncTrack
	curTrack      syncTrack
	curIndex      int
	totalTracks   int
	errors        []syncTrack
	syncStatus    string
	activePlID    string
	cancel        context.CancelFunc
	colorIdx      int

	// add playlist
	addInput string
	addErr   string

	// delete playlist
	deleteID      string
	deleteWithDir bool

	// settings
	settingsField   int
	settingsEditing bool
	settingsInput   string
	settingsMsg     string
}

type tickMsg struct{}

func InitialModel(cfg *config.Config) model {
	cleanDups(cfg)
	return model{
		cfg:  cfg,
		page: homePage,
	}
}

func cleanDups(cfg *config.Config) {
	seen := make(map[string]bool)
	unique := make([]config.Playlist, 0, len(cfg.Playlists))
	for _, pl := range cfg.Playlists {
		if !seen[pl.ID] {
			seen[pl.ID] = true
			unique = append(unique, pl)
		}
	}
	if len(unique) != len(cfg.Playlists) {
		cfg.Playlists = unique
		cfg.Save()
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case startSyncMsg:
		return m.handleStartSync(msg)
	case sync.TrackUpdate:
		return m.handleTrackUpdate(msg), nil
	case sync.StatusMsg:
		plName := msg.PlaylistID
		for _, pl := range m.cfg.Playlists {
			if pl.ID == msg.PlaylistID {
				plName = pl.Name
				break
			}
		}
		m.syncStatus = fmt.Sprintf("[%s] %s", plName, msg.Message)
		return m, nil
	case syncDoneMsg:
		return m.handleSyncDone(msg), nil
	case errMsg:
		m.syncStatus = fmt.Sprintf("Error: %v", msg.err)
		return m, nil
	case tickMsg:
		if m.page == syncPage && !m.syncDone {
			m.colorIdx++
			return m, m.tickCmd()
		}
		return m, nil
	}
	return m, nil
}

type startSyncMsg struct {
	all bool
	id  string
}
type syncDoneMsg struct {
	err     error
	added   int
	removed int
}
type errMsg struct {
	err error
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.page {
	case homePage:
		return m.handleHomeKey(msg)
	case syncPage:
		return m.handleSyncKey(msg)
	case addPage:
		return m.handleAddKey(msg)
	case deletePage:
		return m.handleDeleteKey(msg)
	case settingsPage:
		return m.handleSettingsKey(msg)
	}
	return m, nil
}

// ---- Home ----

func (m model) handleHomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		total := m.homeItemCount()
		if m.cursor < total-1 {
			m.cursor++
		}
		return m, nil
	case "d":
		return m.handleDeleteStart()
	case "enter":
		if m.cursor == m.homeItemCount()-1 {
			return m, tea.Quit
		}
		return m.homeEnter(), nil
	}
	return m, nil
}

func (m model) homeItemCount() int {
	return len(m.cfg.Playlists) + 5
}

func (m model) homeEnter() tea.Model {
	plLen := len(m.cfg.Playlists)
	if m.cursor < plLen {
		pl := m.cfg.Playlists[m.cursor]
		if !pl.Enabled {
			return m
		}
		return m.startSync(startSyncMsg{id: pl.ID})
	}

	switch m.cursor - plLen {
	case 0:
		m.page = addPage
		m.addInput = ""
		m.addErr = ""
		return m
	case 1:
		return m.startSync(startSyncMsg{all: true})
	case 2:
		if service.IsInstalled() {
			service.Remove()
		} else {
			service.Create()
		}
		return m
	case 3:
		m.page = settingsPage
		m.settingsField = 0
		m.settingsEditing = false
		m.settingsInput = ""
		m.settingsMsg = ""
		return m
	}
	return m
}

func (m model) handleDeleteStart() (tea.Model, tea.Cmd) {
	plLen := len(m.cfg.Playlists)
	if m.cursor >= plLen {
		return m, nil
	}
	m.page = deletePage
	m.deleteID = m.cfg.Playlists[m.cursor].ID
	m.deleteWithDir = false
	return m, nil
}

func (m model) handleDeleteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "d":
		m.deleteWithDir = true
		return m.executeDelete(), nil
	case "k":
		m.deleteWithDir = false
		return m.executeDelete(), nil
	case "esc", "q":
		m.page = homePage
		return m, nil
	}
	return m, nil
}

func (m model) executeDelete() model {
	for i, pl := range m.cfg.Playlists {
		if pl.ID != m.deleteID {
			continue
		}
		m.cfg.Playlists = append(m.cfg.Playlists[:i], m.cfg.Playlists[i+1:]...)
		m.cfg.Save()

		if m.deleteWithDir {
			playlistDir := filepath.Join(m.cfg.MusicDir, pl.ID)
			os.RemoveAll(playlistDir)
		}

		if m.cursor >= len(m.cfg.Playlists) && m.cursor > 0 {
			m.cursor--
		}
		break
	}
	m.page = homePage
	return m
}

// ---- Sync ----

func (m model) tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m model) startSync(msg startSyncMsg) tea.Model {
	ctx, cancel := context.WithCancel(context.Background())
	m.page = syncPage
	m.syncDone = false
	m.syncTracks = nil
	m.curTrack = syncTrack{}
	m.curIndex = 0
	m.totalTracks = 0
	m.errors = nil
	m.syncStatus = "Starting..."
	m.activePlID = ""
	m.cancel = cancel
	m.colorIdx = 0
	go runSync(m.cfg, msg, ctx, cancel)
	return m
}

func (m model) handleStartSync(msg startSyncMsg) (tea.Model, tea.Cmd) {
	return m.startSync(msg), m.tickCmd()
}

func (m model) handleSyncKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		if m.cancel != nil {
			m.cancel()
		}
		m.page = homePage
		return m, nil
	}
	return m, nil
}

func (m model) handleTrackUpdate(u sync.TrackUpdate) tea.Model {
	m.activePlID = u.PlaylistID
	m.curIndex = u.Current
	if u.Total > 0 {
		m.totalTracks = u.Total
	}
	m.curTrack = syncTrack{
		title:    u.Title,
		url:      u.URL,
		status:   u.Status,
		progress: u.Progress,
		err:      u.Err,
	}

	m.syncTracks = append(m.syncTracks, m.curTrack)

	if u.Status == sync.Error && u.Err != "" {
		already := false
		for _, e := range m.errors {
			if e.title == u.Title {
				already = true
				break
			}
		}
		if !already {
			m.errors = append(m.errors, m.curTrack)
		}
	}

	return m
}

func (m model) handleSyncDone(d syncDoneMsg) tea.Model {
	m.syncDone = true
	if d.err != nil {
		m.syncStatus = fmt.Sprintf("Error: %v", d.err)
	} else {
		parts := []string{}
		if d.added > 0 {
			parts = append(parts, fmt.Sprintf("+%d", d.added))
		}
		if d.removed > 0 {
			parts = append(parts, fmt.Sprintf("-%d", d.removed))
		}
		errCount := len(m.errors)
		if errCount > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", errCount, plural(errCount, "error", "errors")))
		}
		if len(parts) > 0 {
			m.syncStatus = fmt.Sprintf("Complete — %s", strings.Join(parts, ", "))
		} else {
			m.syncStatus = "Complete"
		}
	}
	return m
}

func plural(n int, s, p string) string {
	if n == 1 {
		return s
	}
	return p
}

// ---- Add ----

func (m model) handleAddKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.confirmAdd()
	case "backspace":
		if len(m.addInput) > 0 {
			m.addInput = m.addInput[:len(m.addInput)-1]
		}
		return m, nil
	case "esc", "q":
		m.page = homePage
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.addInput += msg.String()
			m.addInput = strings.ReplaceAll(m.addInput, "\x1b[200~", "")
			m.addInput = strings.ReplaceAll(m.addInput, "\x1b[201~", "")
			m.addInput = strings.ReplaceAll(m.addInput, "[200~", "")
			m.addInput = strings.ReplaceAll(m.addInput, "[201~", "")
			m.addInput = strings.ReplaceAll(m.addInput, "[200", "")
			m.addInput = strings.ReplaceAll(m.addInput, "[201", "")
		}
		return m, nil
	}
}

func (m model) confirmAdd() (tea.Model, tea.Cmd) {
	id := strings.TrimSpace(m.addInput)
	if id == "" {
		m.addErr = "name cannot be empty"
		return m, nil
	}

	for _, pl := range m.cfg.Playlists {
		if pl.ID == id {
			m.addErr = fmt.Sprintf("playlist '%s' already exists", id)
			return m, nil
		}
	}

	urlPath := id
	if strings.Contains(id, "://") {
		urlPath = id
	} else if !strings.Contains(id, "/") {
		urlPath = "sets/" + id
	}

	m.cfg.Playlists = append(m.cfg.Playlists, config.Playlist{
		ID:      id,
		Name:    id,
		URLPath: urlPath,
		Enabled: true,
	})
	m.cfg.Save()
	m.page = homePage
	return m, nil
}

// ---- Settings ----

func (m model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settingsEditing {
		switch msg.String() {
		case "enter":
			return m.confirmSettings()
		case "esc", "q":
			m.settingsEditing = false
			m.settingsInput = ""
			m.settingsMsg = ""
			return m, nil
		case "backspace":
			if len(m.settingsInput) > 0 {
				m.settingsInput = m.settingsInput[:len(m.settingsInput)-1]
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes {
				m.settingsInput += msg.String()
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "up", "k":
		if m.settingsField > 0 {
			m.settingsField--
		}
		return m, nil
	case "down", "j":
		if m.settingsField < 1 {
			m.settingsField++
		}
		return m, nil
	case "e":
		m.settingsEditing = true
		m.settingsMsg = ""
		if m.settingsField == 0 {
			m.settingsInput = m.cfg.Username
		} else {
			m.settingsInput = m.cfg.MusicDir
		}
		return m, nil
	case "esc", "q":
		m.page = homePage
		return m, nil
	}
	return m, nil
}

func (m model) confirmSettings() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.settingsInput)
	if val == "" {
		m.settingsMsg = "value cannot be empty"
		return m, nil
	}
	if m.settingsField == 0 {
		m.cfg.Username = val
	} else {
		m.cfg.MusicDir = val
	}
	m.cfg.Save()
	m.settingsEditing = false
	m.settingsInput = ""
	m.settingsMsg = "✓ saved"
	return m, nil
}

func (m model) settingsView() string {
	var b strings.Builder
	b.WriteString("sc-sync — Settings\n\n")

	labels := []string{"SoundCloud username", "Music directory"}

	if m.settingsEditing {
		b.WriteString(fmt.Sprintf("Edit %s:\n\n", labels[m.settingsField]))
		b.WriteString("> " + m.settingsInput + "_")
		if m.settingsMsg != "" {
			b.WriteString(fmt.Sprintf("\n\n%s", m.settingsMsg))
		}
		b.WriteString("\n\n[enter] save  [esc] cancel")
	} else {
		vals := []string{m.cfg.Username, m.cfg.MusicDir}
		for i := range labels {
			cur := "  "
			if i == m.settingsField {
				cur = "▸ "
			}
			b.WriteString(fmt.Sprintf("%s%s: %s\n", cur, labels[i], vals[i]))
		}
		if m.settingsMsg != "" {
			b.WriteString(fmt.Sprintf("\n%s", m.settingsMsg))
		}
		b.WriteString("\n\n[j/k] navigate  [e] edit  [esc] back")
	}

	return b.String()
}

// ---- Views ----

func (m model) View() string {
	switch m.page {
	case homePage:
		return m.homeView()
	case syncPage:
		return m.syncView()
	case addPage:
		return m.addView()
	case deletePage:
		return m.deleteView()
	case settingsPage:
		return m.settingsView()
	}
	return ""
}

func (m model) homeView() string {
	var b strings.Builder
	b.WriteString("sc-sync — SoundCloud Sync\n\n")

	for i, pl := range m.cfg.Playlists {
		cur := "  "
		if i == m.cursor {
			cur = "▸ "
		}
		status := "✗"
		if pl.Enabled {
			status = "✓"
		}
		b.WriteString(fmt.Sprintf("%s[%s] %s (%s)\n", cur, status, pl.Name, pl.ID))
	}

	b.WriteString("\n")
	base := len(m.cfg.Playlists)
	actions := []string{
		"+ Add playlist",
		"▶ Sync all",
		service.Status(),
		"⚙ Settings",
		"q  Quit",
	}
	for i, act := range actions {
		cur := "  "
		if base+i == m.cursor {
			cur = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cur, act))
	}

	b.WriteString("\n[j/k] nav  [enter] select  [d] del playlist  [q] quit")
	return b.String()
}

var barColors = []string{"#FF0000", "#FF5F00", "#FFAF00", "#FFFF00", "#AFD700", "#5FD700", "#00D700", "#00D7AF", "#00AFFF", "#005FFF", "#5F00FF", "#AF00FF"}

func colorSquare(idx int) string {
	c := barColors[idx%len(barColors)]
	r, g, b := hexRGB(c)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\u2588\x1b[0m", r, g, b)
}

func hexRGB(s string) (int, int, int) {
	if len(s) == 7 && s[0] == '#' {
		var r, g, b int
		fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
		return r, g, b
	}
	return 255, 255, 255
}

func progressBar(pct float64, width int) string {
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	var b strings.Builder
	b.WriteRune('[')
	for i := 0; i < width; i++ {
		if i < filled {
			b.WriteRune('#')
		} else {
			b.WriteRune('-')
		}
	}
	b.WriteRune(']')
	return b.String()
}

func trackDisplayName(title, url string, idx int) string {
	if title != "" {
		return title
	}
	// extract readable part from URL
	if url != "" {
		parts := strings.Split(strings.TrimRight(url, "/"), "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			if last != "" {
				return last
			}
		}
	}
	return fmt.Sprintf("#%d", idx)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func (m model) syncView() string {
	plName := m.activePlID
	for _, pl := range m.cfg.Playlists {
		if pl.ID == m.activePlID {
			plName = pl.Name
			break
		}
	}

	var b strings.Builder

	if !m.syncDone {
		ct := m.curTrack
		pct := ct.progress
		if pct < 0.01 && ct.status == sync.Downloading {
			pct = 0.05
		}
		bar := progressBar(pct, 20)
		idx := m.curIndex
		if idx == 0 {
			idx = len(m.syncTracks)
		}
		total := m.totalTracks
		if total == 0 {
			total = idx
		}

		label := ""
		switch ct.status {
		case sync.Done:
			label = " ✓"
		case sync.Skipped:
			label = " ↷"
		case sync.Error:
			label = " ✗"
		}

		name := trackDisplayName(ct.title, ct.url, idx)
		b.WriteString(fmt.Sprintf("%s %s · %d/%d · %s %s%s",
			colorSquare(m.colorIdx), plName, idx, total, name, bar, label))
		b.WriteString("  [q] stop")
	} else {
		b.WriteString(fmt.Sprintf("sc-sync — %s\n", m.syncStatus))
		if len(m.errors) > 0 {
			b.WriteString("\n Errors:")
			for _, e := range m.errors {
				eName := trackDisplayName(e.title, e.url, 0)
				errShort := truncate(e.err, 60)
				b.WriteString(fmt.Sprintf("\n  • %s — %s", truncate(eName, 35), errShort))
			}
		}
		b.WriteString("\n\n [esc] back")
	}

	return b.String()
}

func (m model) addView() string {
	var b strings.Builder
	b.WriteString("sc-sync — Add playlist\n\n")
	b.WriteString("Enter playlist name or URL path:\n")
	b.WriteString("(e.g. 'myplaylist' → sets/myplaylist, full URL for any)\n\n")
	b.WriteString("> " + m.addInput + "_")
	if m.addErr != "" {
		b.WriteString(fmt.Sprintf("\n\n%s\n", m.addErr))
	}
	b.WriteString("\n\n[enter] confirm  [esc] cancel")
	return b.String()
}

func (m model) deleteView() string {
	plName := m.deleteID
	for _, pl := range m.cfg.Playlists {
		if pl.ID == m.deleteID {
			plName = pl.Name
			break
		}
	}

	var b strings.Builder
	b.WriteString("sc-sync — Delete playlist\n\n")
	b.WriteString(fmt.Sprintf("Delete '%s'?\n\n", plName))
	b.WriteString("[d] delete with files on disk\n")
	b.WriteString("[k] keep files, remove from list only\n")
	b.WriteString("[esc] cancel")
	return b.String()
}

func runSync(cfg *config.Config, msg startSyncMsg, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()

	database, err := db.Init(cfg.DBPath)
	if err != nil {
		if program != nil {
			program.Send(errMsg{fmt.Errorf("db: %w", err)})
		}
		return
	}
	defer database.Close()

	syncer := sync.New(cfg, database)

	updates := make(chan sync.TrackUpdate, 200)
	statusCh := make(chan sync.StatusMsg, 50)

	go func() {
		for u := range updates {
			if program != nil {
				program.Send(u)
			}
		}
	}()

	go func() {
		for s := range statusCh {
			if program != nil {
				program.Send(s)
			}
		}
	}()

	var syncErr error
	if msg.all {
		syncErr = syncer.SyncAll(ctx, updates, statusCh)
	} else {
		for _, pl := range cfg.Playlists {
			if pl.ID == msg.id {
				syncErr = syncer.SyncPlaylist(ctx, pl, updates, statusCh)
				break
			}
		}
	}

	close(updates)
	close(statusCh)

	ext := "." + cfg.AudioFormat
	if err := playlist.Generate(cfg.MusicDir, ext); err != nil {
		log.Printf("m3u: %v", err)
	}

	if syncErr != nil && syncErr != context.Canceled {
		if program != nil {
			added, removed, _ := syncer.Stats()
			program.Send(syncDoneMsg{err: syncErr, added: added, removed: removed})
		}
		return
	}

	if program != nil {
		added, removed, _ := syncer.Stats()
		program.Send(syncDoneMsg{added: added, removed: removed})
	}
}
