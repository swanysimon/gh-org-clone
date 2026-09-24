package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// initTestRepo creates a real git repo in a fresh temp dir with one commit
// on "main" and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		if _, err := execCommand(context.Background(), dir, "git", args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "--quiet", "-b", "main")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "--quiet", "-m", "initial")
	return dir
}

func testConfig(t *testing.T, root string) config {
	cfg := defaultConfig()
	cfg.Root = root
	cfg.Org = "testorg"
	cfg.Timeout = 30 * time.Second
	return cfg
}

func mustMkReposDir(t *testing.T, cfg config) {
	t.Helper()
	if err := os.MkdirAll(reposDir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestCloneThenFetch(t *testing.T) {
	origin := initTestRepo(t)
	cfg := testConfig(t, t.TempDir())
	mustMkReposDir(t, cfg)

	repo := ghRepo{Name: "repo1", URL: "file://" + origin}
	cfg.Protocol = "https"

	ctx := context.Background()
	if err := cloneRepo(ctx, cfg, repo); err != nil {
		t.Fatalf("cloneRepo: %v", err)
	}
	dir := filepath.Join(reposDir(cfg), "repo1")

	entries, err := os.ReadDir(reposDir(cfg))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "repo1" {
			t.Fatalf("unexpected leftover entry in reposDir: %s", e.Name())
		}
	}

	// New commit upstream.
	if _, err := execCommand(ctx, origin, "git", "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "file.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "add", "file.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "commit", "--quiet", "-m", "second"); err != nil {
		t.Fatal(err)
	}

	if err := fetchRepo(ctx, cfg, dir); err != nil {
		t.Fatalf("fetchRepo: %v", err)
	}
	if warn, _, err := updateWorktree(ctx, cfg, dir, "main"); err != nil || warn != "" {
		t.Fatalf("updateWorktree: warn=%q err=%v", warn, err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2\n" {
		t.Fatalf("working tree file = %q, want %q", got, "v2\n")
	}
}

func TestUpdateWorktreeDirty(t *testing.T) {
	origin := initTestRepo(t)
	cfg := testConfig(t, t.TempDir())
	mustMkReposDir(t, cfg)
	cfg.Protocol = "https"

	ctx := context.Background()
	repo := ghRepo{Name: "repo1", URL: "file://" + origin}
	if err := cloneRepo(ctx, cfg, repo); err != nil {
		t.Fatalf("cloneRepo: %v", err)
	}
	dir := filepath.Join(reposDir(cfg), "repo1")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(origin, "file.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "add", "file.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "commit", "--quiet", "-m", "second"); err != nil {
		t.Fatal(err)
	}
	if err := fetchRepo(ctx, cfg, dir); err != nil {
		t.Fatalf("fetchRepo: %v", err)
	}

	warn, _, err := updateWorktree(ctx, cfg, dir, "main")
	if err != nil {
		t.Fatalf("updateWorktree: %v", err)
	}
	if warn == "" {
		t.Fatalf("expected a warning for a dirty worktree")
	}

	got, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "dirty\n" {
		t.Fatalf("uncommitted edit was overwritten: got %q", got)
	}
}

func TestUpdateWorktreeDetached(t *testing.T) {
	origin := initTestRepo(t)
	cfg := testConfig(t, t.TempDir())
	mustMkReposDir(t, cfg)
	cfg.Protocol = "https"

	ctx := context.Background()
	repo := ghRepo{Name: "repo1", URL: "file://" + origin}
	if err := cloneRepo(ctx, cfg, repo); err != nil {
		t.Fatalf("cloneRepo: %v", err)
	}
	dir := filepath.Join(reposDir(cfg), "repo1")

	if _, err := execCommand(ctx, dir, "git", "checkout", "--quiet", "--detach"); err != nil {
		t.Fatal(err)
	}

	warn, _, err := updateWorktree(ctx, cfg, dir, "main")
	if err != nil {
		t.Fatalf("updateWorktree: %v", err)
	}
	if warn == "" {
		t.Fatalf("expected a warning for a detached HEAD")
	}
}

func TestHeadInfoAndTags(t *testing.T) {
	origin := initTestRepo(t)
	cfg := testConfig(t, t.TempDir())
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(origin, "file2.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "add", "file2.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "commit", "--quiet", "-m", "subject\twith\ttabs"); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "tag", "v1-lightweight"); err != nil {
		t.Fatal(err)
	}
	if _, err := execCommand(ctx, origin, "git", "tag", "-a", "v2-annotated", "-m", "release"); err != nil {
		t.Fatal(err)
	}

	sha, committedAt, subject, err := headInfo(ctx, cfg, origin, "main")
	if err != nil {
		t.Fatalf("headInfo: %v", err)
	}
	if sha == "" {
		t.Fatalf("expected a non-empty sha")
	}
	if subject != "subject\twith\ttabs" {
		t.Fatalf("subject = %q, want tab-containing subject intact", subject)
	}
	if committedAt.IsZero() {
		t.Fatalf("expected a non-zero committedAt")
	}

	tagList, err := tags(ctx, cfg, origin)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if len(tagList) != 2 {
		t.Fatalf("got %d tags, want 2: %+v", len(tagList), tagList)
	}
	names := map[string]bool{}
	for _, tg := range tagList {
		names[tg.Name] = true
		if tg.SHA == "" {
			t.Fatalf("tag %q has empty sha", tg.Name)
		}
		if tg.CreatedAt.IsZero() {
			t.Fatalf("tag %q has zero creation time", tg.Name)
		}
	}
	if !names["v1-lightweight"] || !names["v2-annotated"] {
		t.Fatalf("missing expected tags: %+v", names)
	}
}

func TestHeadInfoEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := execCommand(context.Background(), dir, "git", "init", "--quiet", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t, t.TempDir())

	sha, committedAt, subject, err := headInfo(context.Background(), cfg, dir, "main")
	if err != nil {
		t.Fatalf("headInfo on empty repo should not error: %v", err)
	}
	if sha != "" || subject != "" || !committedAt.IsZero() {
		t.Fatalf("expected zero values for empty repo, got sha=%q subject=%q committedAt=%v", sha, subject, committedAt)
	}
}

func TestLinkedWorktrees(t *testing.T) {
	dir := initTestRepo(t)
	cfg := testConfig(t, t.TempDir())

	got, err := linkedWorktrees(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("linkedWorktrees: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh repo should have no linked worktrees, got %v", got)
	}

	wtPath := filepath.Join(t.TempDir(), "wt")
	if _, err := execCommand(context.Background(), dir, "git", "worktree", "add", "-b", "feature", wtPath, "main"); err != nil {
		t.Fatalf("git worktree add: %v", err)
	}

	got, err = linkedWorktrees(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("linkedWorktrees: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(wtPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != wantPath {
		t.Fatalf("linkedWorktrees = %v, want [%s]", got, wantPath)
	}
}
