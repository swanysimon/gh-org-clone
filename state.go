package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const stateVersion = 1

type repoStatus string

const (
	statusCloned   repoStatus = "cloned"
	statusArchived repoStatus = "archived"
)

type state struct {
	Version   int                  `json:"version"`
	Org       string               `json:"org"`
	UpdatedAt time.Time            `json:"updatedAt"`
	Repos     map[string]repoState `json:"repos"`
}

// repoState.PushedAt is written only after a fully successful sync of that
// repo, so a failed repo is automatically eligible again next run with no
// separate retry bookkeeping.
type repoState struct {
	ID          string     `json:"id"`
	PushedAt    time.Time  `json:"pushedAt"`
	SyncedAt    time.Time  `json:"syncedAt"`
	Status      repoStatus `json:"status"`
	ArchivePath string     `json:"archivePath,omitempty"`
}

func orgDir(cfg config) string      { return filepath.Join(cfg.Root, cfg.Org) }
func reposDir(cfg config) string    { return filepath.Join(orgDir(cfg), "repos") }
func archivesDir(cfg config) string { return filepath.Join(orgDir(cfg), "archives") }
func statePath(cfg config) string   { return filepath.Join(orgDir(cfg), "state.json") }
func lockPath(cfg config) string    { return filepath.Join(orgDir(cfg), "lock") }

// loadState never errors: a missing file yields an empty state, and a file
// that fails to parse or carries the wrong version warns and yields an empty
// state. Losing the cache costs one re-verify pass and destroys nothing,
// which is strictly better than failing the run.
func loadState(path, org string, stderr io.Writer) state {
	empty := state{Version: stateVersion, Org: org, Repos: map[string]repoState{}}

	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}

	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Fprintf(stderr, "warning: state file %s is corrupt, starting fresh: %v\n", path, err)
		return empty
	}
	if s.Version != stateVersion {
		fmt.Fprintf(stderr, "warning: state file %s has version %d, expected %d, starting fresh\n", path, s.Version, stateVersion)
		return empty
	}
	if s.Repos == nil {
		s.Repos = map[string]repoState{}
	}
	return s
}

// saveState writes atomically: temp file in the same directory, fsync,
// close, rename, then fsync the directory so the rename itself is durable.
func saveState(path string, s state) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "state-*.json")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("renaming state file into place: %w", err)
	}

	dirHandle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening state dir to sync: %w", err)
	}
	defer dirHandle.Close()
	if err := dirHandle.Sync(); err != nil {
		return fmt.Errorf("syncing state dir: %w", err)
	}
	return nil
}

// validRepoName is a trust boundary: the name comes from the GitHub API and
// becomes a path segment and a git argument. The first character may be
// alphanumeric or "." (GitHub's own ".github" repo convention) but never
// "-", which blocks the name from being read by git as a flag.
var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9.][A-Za-z0-9._-]*$`)

func validRepoName(name string) bool {
	if name == "." || name == ".." {
		return false
	}
	return repoNamePattern.MatchString(name)
}
