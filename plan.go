package main

type action string

const (
	actionSkip          action = "skip"
	actionClone         action = "clone"
	actionFetch         action = "fetch"
	actionArchive       action = "archive"
	actionAdoptArchived action = "adopt-archived" // archive already on disk, just record it
	actionUnarchive     action = "unarchive"      // was archived locally, now live upstream
	actionNotARepo      action = "not-a-repo"     // dir exists, no .git — report, touch nothing
)

// decide is pure: no filesystem, no subprocess, no clock. All filesystem
// facts arrive as parameters so the whole decision matrix is table-testable.
func decide(repo ghRepo, prev repoState, known, dirExists, isGitDir, manifestExists bool, cfg config) (action, string) {
	if dirExists && !isGitDir {
		return actionNotARepo, "directory exists but is not a git repository"
	}

	if repo.IsArchived && cfg.Archive {
		if manifestExists && !dirExists {
			return actionAdoptArchived, "archive manifest already on disk"
		}
		return actionArchive, "repo is archived upstream"
	}

	if !repo.IsArchived && known && prev.Status == statusArchived {
		return actionUnarchive, "repo was archived locally but is live upstream again"
	}

	// AIDEV: pushedAt does not move for every conceivable upstream ref
	// change, so -force is the escape hatch; upgrade path is a periodic
	// full ls-remote verification pass.
	if known && !cfg.Force && prev.PushedAt.Equal(repo.PushedAt) && dirExists && isGitDir && prev.Status == statusCloned {
		return actionSkip, "pushedAt unchanged since last sync"
	}

	if !dirExists {
		return actionClone, "no local clone exists"
	}

	return actionFetch, "local clone exists and may be stale"
}
