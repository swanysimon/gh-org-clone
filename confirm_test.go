package main

import (
	"os"
	"testing"
)

func TestDefaultConfirmArchiveWithWorktreesYes(t *testing.T) {
	ok, err := defaultConfirmArchiveWithWorktrees(config{Yes: true}, "repo1", []worktreeStatus{{Path: "/tmp/wt"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("cfg.Yes=true should answer yes without touching stdin")
	}
}

func TestDefaultConfirmArchiveWithWorktreesNonInteractive(t *testing.T) {
	// /dev/null is a character device but must never be treated as a
	// terminal a human could answer a prompt on; it's exactly what cron and
	// CI redirect stdin from.
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isInteractive(f) {
		t.Fatalf("/dev/null must never be treated as interactive")
	}

	oldStdin := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = oldStdin })

	ok, err := defaultConfirmArchiveWithWorktrees(config{}, "repo1", []worktreeStatus{{Path: "/tmp/wt"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("a non-interactive run with cfg.Yes=false must never answer yes on its own")
	}
}
