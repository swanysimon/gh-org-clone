package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := state{
		Version:   stateVersion,
		Org:       "myorg",
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Repos: map[string]repoState{
			"one": {ID: "R_one", PushedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), SyncedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Status: statusCloned},
			"two": {ID: "R_two", PushedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), SyncedAt: time.Date(2025, 6, 2, 0, 0, 0, 0, time.UTC), Status: statusArchived, ArchivePath: "archives/two.tar.gz"},
		},
	}

	if err := saveState(path, s); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	loaded := loadState(path, "myorg", os.Stderr)
	if loaded.Version != s.Version || loaded.Org != s.Org || !loaded.UpdatedAt.Equal(s.UpdatedAt) {
		t.Fatalf("top-level fields mismatch: got %+v", loaded)
	}
	if len(loaded.Repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(loaded.Repos))
	}
	for name, want := range s.Repos {
		got, ok := loaded.Repos[name]
		if !ok {
			t.Fatalf("missing repo %q", name)
		}
		if got.ID != want.ID || !got.PushedAt.Equal(want.PushedAt) || !got.SyncedAt.Equal(want.SyncedAt) || got.Status != want.Status || got.ArchivePath != want.ArchivePath {
			t.Fatalf("repo %q mismatch: got %+v, want %+v", name, got, want)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "2026-01-02T03:04:05Z") {
		t.Fatalf("expected RFC3339 timestamps in state file, got: %s", raw)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Fatalf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestLoadStateCorrupt(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name    string
		content []byte // nil means "do not create the file"
	}{
		{"missing file", nil},
		{"garbage", []byte("not json at all")},
		{"wrong version", []byte(`{"version": 99, "org": "myorg", "repos": {}}`)},
		{"nil repos", []byte(`{"version": 1, "org": "myorg", "repos": null}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".json")
			if tc.content != nil {
				if err := os.WriteFile(path, tc.content, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			var stderrBuf bytes.Buffer
			s := loadState(path, "myorg", &stderrBuf)

			if s.Repos == nil {
				t.Fatalf("Repos map must never be nil")
			}
			if tc.name == "garbage" || tc.name == "wrong version" {
				if stderrBuf.Len() == 0 {
					t.Fatalf("expected a warning on stderr for %s", tc.name)
				}
			}
		})
	}
}

func TestValidRepoName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"repo", true},
		{".github", true},
		{"foo.bar", true},
		{"a-b", true},
		{"x_y", true},
		{"a1", true},
		{"", false},
		{".", false},
		{"..", false},
		{"a/b", false},
		{"-x", false},
		{"--upload-pack=x", false},
		{"é", false},
		{"a b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validRepoName(tc.name); got != tc.ok {
				t.Fatalf("validRepoName(%q) = %v, want %v", tc.name, got, tc.ok)
			}
		})
	}
}
