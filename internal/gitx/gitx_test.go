package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		git(t, dir, args...)
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFindOutsideRepo(t *testing.T) {
	if _, err := Find(t.TempDir()); err != ErrNotRepo {
		t.Fatalf("want ErrNotRepo, got %v", err)
	}
}

func TestEmptyRepo(t *testing.T) {
	r, err := Find(gitInit(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.RootCommit() != "" || r.Remote() != "" {
		t.Fatal("empty repo should have no root commit or remote")
	}
	if r.Branch() != "main" {
		t.Fatalf("branch = %q", r.Branch())
	}
}

func TestIdentity(t *testing.T) {
	dir := gitInit(t)
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "second")
	git(t, dir, "remote", "add", "origin", "https://example.com/x.git")

	r, _ := Find(dir)
	root := r.RootCommit()
	if len(root) != 40 {
		t.Fatalf("root commit = %q", root)
	}
	if r.Remote() != "https://example.com/x.git" {
		t.Fatalf("remote = %q", r.Remote())
	}
}

func TestRelPath(t *testing.T) {
	dir := gitInit(t)
	sub := filepath.Join(dir, "src")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, _ := Find(sub)
	cases := map[string]string{
		"cache.ts":                        "src/cache.ts",
		"../README.md":                    "README.md",
		filepath.Join(dir, "src", "a.go"): "src/a.go",
		"not/created/yet.go":              "src/not/created/yet.go",
	}
	for in, want := range cases {
		if got := r.RelPath(sub, in); got != want {
			t.Errorf("RelPath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := r.RelPath(sub, "/elsewhere/x.go"); got != "/elsewhere/x.go" {
		t.Errorf("outside path changed: %q", got)
	}
}

func TestChangedFilesAndPatchID(t *testing.T) {
	dir := gitInit(t)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a b.txt", "one\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "root commit")
	r, _ := Find(dir)

	root, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.ChangedFiles(root)
	if err != nil || len(files) != 1 || files[0] != "a b.txt" {
		t.Fatalf("root commit files = %q, err %v", files, err)
	}

	// The same change on two branches has one patch ID but two commit hashes.
	git(t, dir, "switch", "-q", "-c", "other")
	write("b.txt", "two\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "change")
	picked, _ := r.Head()
	git(t, dir, "switch", "-q", "main")
	write("c.txt", "three\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "move main")
	git(t, dir, "cherry-pick", picked)
	copied, _ := r.Head()
	if copied == picked {
		t.Fatal("cherry-pick kept the hash")
	}
	if a, b := r.PatchID(picked), r.PatchID(copied); a == "" || a != b {
		t.Fatalf("patch IDs differ: %q %q", a, b)
	}
}
