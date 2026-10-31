package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/decision-log/decision-log/internal/note"
)

func TestSaveAndAll(t *testing.T) {
	home := t.TempDir()
	s, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)
	older := note.New("A", "w", "r", "user", nil, t0)
	newer := note.New("B", "w", "r", "user", nil, t0.Add(time.Minute))
	for _, n := range []*note.Note{older, newer} {
		if err := s.Save(n); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Decision != "B" {
		t.Fatalf("want newest first, got %+v", got)
	}

	if runtime.GOOS != "windows" {
		assertMode(t, home, 0o700)
		assertMode(t, filepath.Join(home, "decisions"), 0o700)
		assertMode(t, s.path(older.ID), 0o600)
	}
}

func TestSaveOverwrites(t *testing.T) {
	s, _ := Open(t.TempDir())
	n := note.New("A", "w", "r", "user", nil, time.Now())
	_ = s.Save(n)
	n.Why = "changed"
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	got, _ := s.All()
	if len(got) != 1 || got[0].Why != "changed" {
		t.Fatalf("got %+v", got)
	}
}

func TestCorruptFileDoesNotHideOthers(t *testing.T) {
	s, _ := Open(t.TempDir())
	_ = s.Save(note.New("A", "w", "r", "user", nil, time.Now()))
	_ = os.WriteFile(filepath.Join(s.dir, "bad.json"), []byte("{"), 0o600)
	got, err := s.All()
	if err == nil {
		t.Fatal("expected an error for the corrupt file")
	}
	if len(got) != 1 {
		t.Fatalf("good note lost: %d notes", len(got))
	}
}

func TestRejectsPathInID(t *testing.T) {
	s, _ := Open(t.TempDir())
	n := note.New("A", "w", "r", "user", nil, time.Now())
	n.ID = "../escape"
	if s.Save(n) == nil {
		t.Fatal("expected an error")
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s: mode %o, want %o", path, got, want)
	}
}
