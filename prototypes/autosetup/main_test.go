package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAddCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	if changed, err := apply(path, addRule); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.Contains(read(t, path), allowRule) {
		t.Fatal(read(t, path))
	}
}

func TestAddKeepsEverythingElseInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"theme":"dark","permissions":{"deny":["Bash(rm:*)"],"allow":["Read"]},"model":"opus","url":"a&b"}`)
	if _, err := apply(path, addRule); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	order := []string{`"theme"`, `"permissions"`, `"deny"`, `"allow"`, `"Read"`, allowRule, `"model"`, `"url"`, `a&b`}
	last := -1
	for _, want := range order {
		i := strings.Index(got, want)
		if i <= last {
			t.Fatalf("%s missing or out of order:\n%s", want, got)
		}
		last = i
	}
}

func TestAddIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"model":"opus"}`)
	apply(path, addRule)
	first := read(t, path)
	if changed, err := apply(path, addRule); err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if read(t, path) != first {
		t.Fatal("second add changed the file")
	}
}

func TestRemoveUndoesAdd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"model":"opus"}`)
	apply(path, addRule)
	apply(path, removeRule)
	if got := strings.Join(strings.Fields(read(t, path)), ""); got != `{"model":"opus"}` {
		t.Fatalf("got %s", got)
	}
}

func TestRemoveKeepsOtherRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"permissions":{"allow":["Read","`+allowRule+`"]}}`)
	apply(path, removeRule)
	got := read(t, path)
	if strings.Contains(got, allowRule) || !strings.Contains(got, `"Read"`) {
		t.Fatal(got)
	}
}

func TestRefusesWhatItDoesNotUnderstand(t *testing.T) {
	for name, in := range map[string]string{
		"comments":       "{\n// mine\n\"model\":\"opus\"}",
		"truncated":      `{"model":`,
		"array":          `[]`,
		"trailing":       `{} {}`,
		"bad permission": `{"permissions":"all"}`,
		"bad allow":      `{"permissions":{"allow":"Read"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			write(t, path, in)
			if _, err := apply(path, addRule); err == nil {
				t.Fatal("expected a refusal")
			}
			if read(t, path) != in {
				t.Fatal("refused file was changed")
			}
			if _, err := os.Stat(path + ".decision-log-backup"); err == nil {
				t.Fatal("refusal left a backup")
			}
		})
	}
}

func TestBackupKeepsTheOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"model":"opus"}`)
	apply(path, addRule)
	apply(path, removeRule)
	apply(path, addRule)
	if got := read(t, path+".decision-log-backup"); got != `{"model":"opus"}` {
		t.Fatalf("backup = %s", got)
	}
}

func TestWritesThroughSymlinkAndKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks and modes differ on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles.json")
	link := filepath.Join(dir, "settings.json")
	write(t, target, `{}`)
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(link, addRule); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v %v", fi, err)
	}
	if !strings.Contains(read(t, target), allowRule) {
		t.Fatal("target not updated")
	}
	st, _ := os.Stat(target)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
}
