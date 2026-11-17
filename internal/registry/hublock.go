package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// HubLock is the election token: whoever holds the flock on run/hub.lock
// is the hub for this state dir (rung 7). The kernel releases it when the
// holder exits, however it exits.
type HubLock struct{ f *os.File }

func (s State) hubLockPath() string { return filepath.Join(s.RunDir(), "hub.lock") }

// TryHubLock takes the hub lock without blocking. ok is false (and err nil)
// when another process holds it.
func (s State) TryHubLock() (*HubLock, bool, error) {
	if err := s.Ensure(); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(s.hubLockPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &HubLock{f: f}, true, nil
}

// Close releases the lock.
func (l *HubLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// HubLockHolder is the pid last written to hub.lock (0 if unknown). It is
// for messages only: liveness is the lock, not the pid.
func (s State) HubLockHolder() int {
	b, err := os.ReadFile(s.hubLockPath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}
