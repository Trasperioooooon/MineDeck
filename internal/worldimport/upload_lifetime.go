package worldimport

import (
	"errors"
	"os"
	"strings"
	"time"

	"minedeck/internal/atomicfile"
	"minedeck/internal/inst"
)

type uploadRef struct {
	path string
	refs int
}

// A reference is held while queued/scanning and then transferred to the cached
// result. Pins extend that result's lifetime. Different results may share a file.
func (s *Service) holdUpload(path string) (string, error) {
	real, err := inst.RealPathStrict(path)
	if err != nil {
		return "", err
	}
	defer atomicfile.LockPath(path)()
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	key := strings.ToLower(real)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploads == nil {
		s.uploads = map[string]*uploadRef{}
	}
	u := s.uploads[key]
	if u == nil {
		u = &uploadRef{path: path}
		s.uploads[key] = u
	}
	u.refs++
	return key, nil
}

func (s *Service) releaseUpload(key string) error {
	s.mu.Lock()
	u := s.uploads[key]
	s.mu.Unlock()
	if u == nil {
		return nil
	}
	defer atomicfile.LockPath(u.path)()
	s.mu.Lock()
	u = s.uploads[key]
	if u == nil {
		s.mu.Unlock()
		return nil
	}
	u.refs--
	last := u.refs == 0
	if last {
		delete(s.uploads, key)
	}
	s.mu.Unlock()
	if !last {
		return nil
	}
	err := os.Remove(u.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err // A failed delete remains an orphan for the next sweep/startup.
}

func (s *Service) removeUnusedUpload(path string, before time.Time) {
	real, err := inst.RealPathStrict(path)
	if err != nil {
		return
	}
	defer atomicfile.LockPath(path)()
	s.mu.Lock()
	used := s.uploads[strings.ToLower(real)] != nil
	s.mu.Unlock()
	if used {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !before.IsZero() && !info.ModTime().Before(before) {
		return
	}
	_ = os.Remove(path)
}
