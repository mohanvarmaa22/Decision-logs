package githook

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/decision-log/decision-log/internal/gitx"
)

type fixture struct {
	t    *testing.T
	dir  string
	r    *gitx.Repo
	home string
	bin  string // a fake decision-log that logs each call
	ran  string
}

func newRepo(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir(), home: t.TempDir()}
	f.git("init", "-q", "-b", "main")
	f.git("config", "user.email", "test@example.com")
	f.git("config", "user.name", "Test")
	f.git("config", "commit.gpgsign", "false")
	r, err := gitx.Find(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	f.r = r
	tools := t.TempDir()
	f.ran = filepath.Join(tools, "ran")
	f.bin = filepath.Join(tools, "decision log") // the space tests quoting
	f.write(f.bin, fmt.Sprintf("#!/bin/sh\necho \"$*\" >> '%s'\n", f.ran), 0o755)
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", f.dir}, args...)...).CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) write(path, body string, mode os.FileMode) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) hook() string { return filepath.Join(f.r.Root, ".git", "hooks", "post-commit") }

func (f *fixture) read(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) commit() {
	f.t.Helper()
	f.git("commit", "-q", "--allow-empty", "-m", "x")
}

func TestInstallsFreshHookThatRuns(t *testing.T) {
	f := newRepo(t)
	if changed, err := Install(f.r, f.bin, f.home); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	f.commit()
	if got := f.read(f.ran); got != "hook post-commit\n" {
		t.Fatalf("ran %q", got)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	f := newRepo(t)
	Install(f.r, f.bin, f.home)
	first := f.read(f.hook())
	if changed, err := Install(f.r, f.bin, f.home); err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if f.read(f.hook()) != first {
		t.Fatal("second install changed the hook")
	}
}

func TestInstallUpdatesMovedBinary(t *testing.T) {
	f := newRepo(t)
	Install(f.r, "/old/decision-log", f.home)
	if changed, err := Install(f.r, f.bin, f.home); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got := f.read(f.hook())
	if strings.Contains(got, "/old/") || strings.Count(got, beginMarker) != 1 {
		t.Fatalf("not replaced:\n%s", got)
	}
}

func TestChainsExistingHookEvenWhenItExits(t *testing.T) {
	f := newRepo(t)
	userRan := filepath.Join(t.TempDir(), "user")
	f.write(f.hook(), fmt.Sprintf("#!/usr/bin/env bash\necho user > '%s'\nexit 7\n", userRan), 0o755)
	if _, err := Install(f.r, f.bin, f.home); err != nil {
		t.Fatal(err)
	}
	f.commit()
	if f.read(userRan) != "user\n" {
		t.Error("user's hook didn't run")
	}
	if f.read(f.ran) != "hook post-commit\n" {
		t.Error("our block didn't run")
	}
}

func TestMissingBinaryDoesNotBreakCommit(t *testing.T) {
	f := newRepo(t)
	Install(f.r, "/nonexistent/decision-log", f.home)
	f.commit() // f.git fails the test if the commit fails
}

func TestRemoveRestoresUserHookExactly(t *testing.T) {
	f := newRepo(t)
	original := "#!/bin/bash\n\nset -e\necho hi\n# no trailing newline"
	f.write(f.hook(), original, 0o750)
	Install(f.r, f.bin, f.home)
	if err := Remove(f.hook()); err != nil {
		t.Fatal(err)
	}
	if got := f.read(f.hook()); got != original {
		t.Fatalf("got %q", got)
	}
	if fi, _ := os.Stat(f.hook()); fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %o", fi.Mode().Perm())
	}
}

func TestRemoveDeletesHookItCreated(t *testing.T) {
	f := newRepo(t)
	Install(f.r, f.bin, f.home)
	if err := Remove(f.hook()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.hook()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hook left behind: %v", err)
	}
	if err := Remove(f.hook()); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}

func TestSkipsWhatItCannotSafelyChange(t *testing.T) {
	cases := map[string]struct {
		body string
		mode os.FileMode
		want error
	}{
		"python hook":   {"#!/usr/bin/env python3\nprint('x')\n", 0o755, ErrNotShell},
		"binary hook":   {"\x7fELF\x00", 0o755, ErrNotShell},
		"disabled hook": {"#!/bin/sh\necho off\n", 0o644, ErrDisabled},
		"damaged block": {"#!/bin/sh\n" + beginMarker + "\necho x\n", 0o755, ErrDamaged},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRepo(t)
			f.write(f.hook(), c.body, c.mode)
			if _, err := Install(f.r, f.bin, f.home); !errors.Is(err, c.want) {
				t.Fatalf("err = %v", err)
			}
			if f.read(f.hook()) != c.body {
				t.Fatal("hook was changed")
			}
		})
	}
}

func TestSkipsSymlinkedHook(t *testing.T) {
	f := newRepo(t)
	target := filepath.Join(t.TempDir(), "shared")
	f.write(target, "#!/bin/sh\n", 0o755)
	if err := os.MkdirAll(filepath.Dir(f.hook()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, f.hook()); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(f.r, f.bin, f.home); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v", err)
	}
	if f.read(target) != "#!/bin/sh\n" {
		t.Fatal("shared hook was changed")
	}
}

func TestSkipsTrackedHooksDir(t *testing.T) {
	f := newRepo(t)
	tracked := filepath.Join(f.dir, ".githooks", "post-commit")
	f.write(tracked, "#!/bin/sh\necho team\n", 0o755)
	f.git("add", ".")
	f.commit()
	f.git("config", "core.hooksPath", ".githooks")
	if _, err := Install(f.r, f.bin, f.home); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("err = %v", err)
	}
	if status := f.git("status", "--short"); status != "" {
		t.Fatalf("repo dirtied: %s", status)
	}
}

func TestSkipsUntrackedHooksDirInsideRepo(t *testing.T) {
	f := newRepo(t)
	f.git("config", "core.hooksPath", ".githooks")
	if _, err := Install(f.r, f.bin, f.home); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("err = %v", err)
	}
	if status := f.git("status", "--short"); status != "" {
		t.Fatalf("repo dirtied: %s", status)
	}
}

func TestSkipsGlobalHooksPath(t *testing.T) {
	f := newRepo(t)
	global := t.TempDir()
	f.git("config", "core.hooksPath", global)
	if _, err := Install(f.r, f.bin, f.home); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "post-commit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote to a shared hooks directory")
	}
}

func TestInstallsIntoIgnoredHooksDirLikeHusky(t *testing.T) {
	f := newRepo(t)
	dir := filepath.Join(f.dir, ".husky", "_")
	f.write(filepath.Join(dir, ".gitignore"), "*\n", 0o644)
	husky := filepath.Join(dir, "post-commit")
	f.write(husky, "#!/usr/bin/env sh\ntrue\n", 0o755)
	f.git("config", "core.hooksPath", ".husky/_")
	if _, err := Install(f.r, f.bin, f.home); err != nil {
		t.Fatal(err)
	}
	f.commit()
	if f.read(f.ran) != "hook post-commit\n" {
		t.Fatal("block didn't run")
	}
	if status := f.git("status", "--short"); status != "" {
		t.Fatalf("repo dirtied: %s", status)
	}
}

func TestWorktreeSharesTheHook(t *testing.T) {
	f := newRepo(t)
	f.commit()
	wt := filepath.Join(t.TempDir(), "wt")
	f.git("worktree", "add", "-q", wt, "-b", "side")
	r, err := gitx.Find(wt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(r, f.bin, f.home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.hook()); err != nil {
		t.Fatalf("worktree install should use the shared hooks dir: %v", err)
	}
}

func TestRecordsHookForUninstall(t *testing.T) {
	f := newRepo(t)
	Install(f.r, f.bin, f.home)
	Install(f.r, f.bin, f.home)
	entries, err := os.ReadDir(filepath.Join(f.home, "hooks"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	got := strings.TrimSpace(f.read(filepath.Join(f.home, "hooks", entries[0].Name())))
	if got != f.hook() {
		t.Fatalf("recorded %q, want %q", got, f.hook())
	}
}
