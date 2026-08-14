package awake

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type StateStore struct{ Dir string }

func NewStateStore(home string) *StateStore {
	return &StateStore{Dir: filepath.Join(home, "Library", "Application Support", "awake")}
}
func (s *StateStore) PIDPath() string    { return filepath.Join(s.Dir, "awake.pid") }
func (s *StateStore) LockPath() string   { return filepath.Join(s.Dir, "awake.lock") }
func (s *StateStore) StatusPath() string { return filepath.Join(s.Dir, "awake.json") }
func (s *StateStore) LogsDir() string    { return filepath.Join(s.Dir, "logs") }
func (s *StateStore) Ensure() error      { return os.MkdirAll(s.LogsDir(), 0700) }

// Acquire creates an atomic, process-owned lock for the lifetime of awake run.
func (s *StateStore) Acquire() (func(), error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.LockPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		if stale, checkErr := s.lockIsStale(); checkErr == nil && stale {
			if removeErr := os.Remove(s.LockPath()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return nil, removeErr
			}
			return s.Acquire()
		}
		return nil, fmt.Errorf("awake is already running (lock %s)", s.LockPath())
	}
	if err != nil {
		return nil, err
	}
	_, err = fmt.Fprintf(f, "%d\n", os.Getpid())
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(s.LockPath())
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return func() { _ = os.Remove(s.LockPath()); _ = os.Remove(s.PIDPath()) }, nil
}
func (s *StateStore) lockIsStale() (bool, error) {
	b, err := os.ReadFile(s.LockPath())
	if err != nil {
		return false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return true, nil
	}
	return !processAlive(pid), nil
}
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

func (s *StateStore) SaveProcess(p ProcessInfo) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	return writeJSONAtomic(s.PIDPath(), p)
}
func (s *StateStore) LoadProcess() (ProcessInfo, error) {
	var p ProcessInfo
	b, err := os.ReadFile(s.PIDPath())
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}
func (s *StateStore) SaveSnapshot(v Snapshot) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	return writeJSONAtomic(s.StatusPath(), v)
}
func writeJSONAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".awake-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
