// Package store keeps one JSON file per note under ~/.decision-log/decisions.
// One file per note means parallel sessions never write the same file, so no
// locking is needed.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/decision-log/decision-log/internal/note"
)

// HomeEnv overrides the data directory; tests and scripts use it.
const HomeEnv = "DECISION_LOG_HOME"

type Store struct {
	dir string
}

// DefaultHome is $DECISION_LOG_HOME, or ~/.decision-log.
func DefaultHome() (string, error) {
	if h := os.Getenv(HomeEnv); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".decision-log"), nil
}

// Open creates the data directories if needed, readable only by the user.
func Open(home string) (*Store, error) {
	dir := filepath.Join(home, "decisions")
	for _, d := range []string{home, dir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		// MkdirAll leaves an existing directory's mode alone.
		if err := os.Chmod(d, 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir}, nil
}

// Save writes n atomically: a temp file in the same directory, then rename.
func (s *Store) Save(n *note.Note) error {
	if n.ID == "" || strings.ContainsAny(n.ID, `/\`) {
		return fmt.Errorf("invalid note id %q", n.ID)
	}
	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path(n.ID))
}

// All returns every readable note, newest first. A corrupt file is skipped and
// reported in the returned error, so one bad file never hides the rest.
func (s *Store) All() ([]*note.Note, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var notes []*note.Note
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		n, err := s.read(filepath.Join(s.dir, name))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		notes = append(notes, n)
	}
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].CreatedAt.After(notes[j].CreatedAt)
	})
	return notes, errors.Join(errs...)
}

func (s *Store) read(path string) (*note.Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var n note.Note
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
