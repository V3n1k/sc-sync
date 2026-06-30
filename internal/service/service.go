package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	serviceName = "sc-sync"
	serviceDir  = ".config/systemd/user"
)

func serviceContent(binaryPath string) string {
	return fmt.Sprintf(`[Unit]
Description=sc-sync SoundCloud Sync

[Service]
Type=oneshot
ExecStart=%s sync --headless
`, binaryPath)
}

func timerContent() string {
	return `[Unit]
Description=Run sc-sync daily

[Timer]
OnCalendar=*-*-* 06:00:00
Persistent=true

[Install]
WantedBy=timers.target
`
}

func servicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, serviceDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, serviceName+".service"), nil
}

func timerFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, serviceDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, serviceName+".timer"), nil
}

func binaryPath() string {
	exe, _ := os.Executable()
	return exe
}

func Create() error {
	binPath := binaryPath()

	svcPath, err := servicePath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(svcPath, []byte(serviceContent(binPath)), 0644); err != nil {
		return fmt.Errorf("write service: %w", err)
	}

	timerPath, err := timerFilePath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(timerPath, []byte(timerContent()), 0644); err != nil {
		return fmt.Errorf("write timer: %w", err)
	}

	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", "--now", filepath.Base(timerPath)); err != nil {
		return err
	}
	return nil
}

func Remove() error {
	_ = runSystemctl("disable", "--now", serviceName+".timer")
	_ = runSystemctl("disable", "--now", serviceName+".service")

	svcPath, _ := servicePath()
	timerPath, _ := timerFilePath()
	os.Remove(svcPath)
	os.Remove(timerPath)

	return runSystemctl("daemon-reload")
}

func IsInstalled() bool {
	svcPath, err := servicePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(svcPath)
	return err == nil
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func Status() string {
	if IsInstalled() {
		return "✓ сервис установлен (06:00 ежедневно)"
	}
	return "✗ сервис не установлен"
}
