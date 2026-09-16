# AIDEV.md — build checklist for `gh-org-clone`

This file is executed by LLM coding tools, one unchecked item at a time. It is not documentation.

## How to use this file

1. Find the first `## Stage N` whose items are not all checked.
2. Run that stage's **jj commit** item first, before editing any file.
3. Do the stage's items in order. Check each one off (`- [ ]` → `- [x]`) only after it is done.
4. Run the stage's **Verify** item. If it fails, fix forward in the same stage; do not move on.
5. Stop at the end of the stage. Do not start the next stage in the same turn.

Every item is self-contained: structs, command lines and conditions are written out in the item that
needs them. Do not go looking in other stages for context.

## Global rules that apply to every stage

- **Zero third-party dependencies.** `go.mod` must never gain a `require` block. Use stdlib `flag` (not
  cobra/urfave), `tar` + `gzip` (not zstd), `encoding/json` for config (not TOML/YAML), stdlib
  `testing` (not testify).
- Flat `package main` at the repo root. No `cmd/`, no `internal/`, no subpackages.
- Strict typing. No `interface{}`/`any` in any signature.
- Comments: only `AIDEV:` comments marking a deliberate simplification and naming its ceiling plus the
  upgrade path. No other comments.
- Every `git` subprocess passes `--` before user-derived arguments, and runs with
  `GIT_TERMINAL_PROMPT=0` in its environment.
- Before checking off a stage's Verify item, `gofmt -l .` must print nothing and `go vet ./...` must
  pass.

---

## Stage 0 — Prerequisites (run by the user, not by a tool)

- [x] Install Go: `brew install go`. Confirm with `go version`. There is currently no Go toolchain on
      this machine, so no later stage can be verified until this is done. (go1.27.1 confirmed installed.)
- [ ] Re-authenticate the GitHub CLI: `gh auth login`. `gh auth status` currently reports an invalid
      token for account `swanysimon`. Only Stage 10's live smoke test needs this; Stages 1–9 are all
      verifiable offline. **Still blocked as of Stage 1** — proceeding with Stages 1–9 offline.
- [ ] Verify: `go version && git --version && gh auth status` all succeed.

---

## Stage 1 — Go module and CLI skeleton

- [ ] `jj new -m "chore: go module and CLI skeleton"`
- [ ] Create `go.mod` with `go mod init github.com/swanysimon/gh-org-clone`, then set the `go` directive
      to the installed toolchain's major.minor (`go version`). Confirm with the user if a different
      module path is wanted; `swanysimon` comes from the authenticated `gh` account. `go.mod` must
      contain no `require` block.
- [ ] Create `.gitignore` containing exactly two lines: `/gh-org-clone` (the built binary) and
      `/coverage.out`.
- [ ] Create `main.go` with `package main` and this shape. `run` takes its writers and args as
      parameters — that is what makes Stage 9's end-to-end test possible, so do not read `os.Args` or
      write to `os.Stdout` anywhere except `main`.
      ```go
      func main() {
      	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
      }

      func run(ctx context.Context, args []string, stdout, stderr io.Writer) int
      ```
- [ ] In `run`, parse args with a `*flag.FlagSet` created via `flag.NewFlagSet("gh-org-clone",
      flag.ContinueOnError)` and `fs.SetOutput(stderr)`. Usage line: `gh-org-clone [flags] <org>`.
      Require exactly one positional argument. On a parse error or wrong argument count, print usage to
      `stderr` and `return 2`. On success `return 0` for now.
- [ ] Define the exit-code contract in a `const` block and use it everywhere from here on: `0` success,
      `1` runtime failure (one or more repos failed), `2` usage or config error, `130` interrupted.
- [ ] Verify: `gofmt -l . && go vet ./... && go build ./... && ./gh-org-clone; test $? -eq 2 && ./gh-org-clone a b; test $? -eq 2`

---

## Stage 2 — Config resolution

- [ ] `jj new -m "feat: config resolution from flags, env, file, defaults"`
- [ ] Add the `config` struct to `main.go`. This is the resolved, validated config — all value types, no
      pointers.
      ```go
      type config struct {
      	Org          string
      	Root         string        // org dir is <Root>/<Org>
      	Concurrency  int
      	Timeout      time.Duration // per subprocess
      	MaxRepos     int           // gh --limit
      	Protocol     string        // "ssh" | "https"
      	IncludeForks bool
      	Archive      bool
      	Force        bool
      	DryRun       bool
      	Verbose      bool
      }
      ```
- [ ] Add the `fileConfig` struct to `main.go`. Every field is a pointer so that `nil` means "not set in
      the file" and an explicit `false`/`0` in the file is distinguishable from absence.
      ```go
      type fileConfig struct {
      	Root         *string `json:"root"`
      	Concurrency  *int    `json:"concurrency"`
      	Timeout      *string `json:"timeout"` // parsed with time.ParseDuration
      	MaxRepos     *int    `json:"maxRepos"`
      	Protocol     *string `json:"protocol"`
      	IncludeForks *bool   `json:"includeForks"`
      	Archive      *bool   `json:"archive"`
      }
      ```
- [ ] Add `defaultRoot() string`: return `$XDG_DATA_HOME/gh-org-clone` if `XDG_DATA_HOME` is set and
      absolute, else `filepath.Join(os.UserHomeDir(), ".local", "share", "gh-org-clone")`.
- [ ] Add `defaultConfig() config` returning: `Root: defaultRoot()`, `Concurrency: 8`, `Timeout: 30 *
      time.Minute`, `MaxRepos: 10000`, `Protocol: "ssh"`, `IncludeForks: false`, `Archive: true`. These
      are the only place defaults are written — do **not** also pass defaults into the `flag` calls
      (pass zero values there), because the next item relies on flag values being meaningless unless
      visited.
- [ ] Add `resolveConfig(fs *flag.FlagSet, args []string, stderr io.Writer) (config, error)` applying
      precedence **flags > env > file > defaults**:
      1. Register flags: `-root`, `-concurrency`, `-timeout` (string), `-max-repos`, `-protocol`,
         `-include-forks`, `-archive`, `-force`, `-dry-run`, `-v`, `-config`. Parse `args`.
      2. Resolve the config file path: `-config` flag, else `$GH_ORG_CLONE_CONFIG`, else
         `$XDG_CONFIG_HOME/gh-org-clone/config.json`, else `~/.config/gh-org-clone/config.json`.
      3. Start from `defaultConfig()`. Overlay non-`nil` `fileConfig` fields. A missing file is skipped
         silently; a file that exists but fails to parse, or has an unparseable `timeout`, is a **hard
         error** — never guess. Use `json.Decoder` with `DisallowUnknownFields` so a typo'd key is an
         error rather than a silently ignored setting.
      4. Overlay these env vars if non-empty: `GH_ORG_CLONE_ROOT`, `GH_ORG_CLONE_CONCURRENCY`,
         `GH_ORG_CLONE_TIMEOUT`, `GH_ORG_CLONE_MAX_REPOS`, `GH_ORG_CLONE_PROTOCOL`,
         `GH_ORG_CLONE_INCLUDE_FORKS`, `GH_ORG_CLONE_ARCHIVE`. Parse with `strconv`/`time.ParseDuration`;
         a malformed value is a hard error.
      5. Overlay flags using `fs.Visit(func(f *flag.Flag) {...})`, which reports **only** the flags the
         user actually typed. This is the mechanism that keeps defaults in exactly one place.
      6. Set `Org` from `fs.Arg(0)`.
- [ ] Add validation at the end of `resolveConfig`, returning an error for any of: empty `Org`, `Org` not
      matching `^[A-Za-z0-9][A-Za-z0-9-]*$`, `Concurrency < 1`, `MaxRepos < 1`, `Timeout <= 0`,
      `Protocol` not in `{"ssh", "https"}`, `Root` not absolute. `Force`, `DryRun` and `Verbose` are
      flag-only by design and must not be readable from env or file.
- [ ] Wire `run` to call `resolveConfig` and `return 2` on error, printing the error to `stderr`.
- [ ] Create `main_test.go` with `TestConfigDefaults` (empty env + no file → `defaultConfig()` values
      plus `Org`) and `TestConfigPrecedence`: using `t.Setenv` and a config file written into
      `t.TempDir()`, assert that (a) file beats default, (b) env beats file, (c) an explicit flag beats
      env, and (d) a flag that is *not* passed does not clobber the file's value. Item (d) is the
      regression that `fs.Visit` exists to prevent.
- [ ] Add `TestConfigRejects` — a table asserting an error for: no positional arg, two positional args,
      `-concurrency 0`, `-protocol ftp`, `-timeout banana`, `-max-repos -1`, a malformed JSON config
      file, and a config file with an unknown key.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -run TestConfig -v ./...`

---

## Stage 3 — List org repos via `gh`

- [ ] `jj new -m "feat: list org repos via gh"`
- [ ] Create `git.go` containing **only** the command seam for now. One package-level function variable
      is the entire test seam for the project — do not introduce an interface, a struct, or dependency
      injection.
      ```go
      var runner = execCommand

      func execCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error)
      ```
      `execCommand` builds `exec.CommandContext`, sets `cmd.Dir = dir` when `dir != ""`, sets
      `cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ADVICE=0")`, captures stdout into a
      buffer and stderr into a separate buffer, and on a non-zero exit returns an error that includes
      the full command line **and** the captured stderr. Inheriting the rest of the environment is
      required so `gh` can find `GH_TOKEN`/`GH_HOST`/the keychain.
- [ ] Create `gh.go` with the listing types. `defaultBranchRef` is a pointer because it is `null` for
      repos with no commits — that is the reliable empty-repo signal. `pushedAt` and `archivedAt` are
      also `null` in real payloads; `json.Unmarshal` leaves the zero `time.Time` in place for `null`, so
      no custom unmarshaller is needed.
      ```go
      type ghRepo struct {
      	ID            string     `json:"id"`
      	Name          string     `json:"name"`
      	NameWithOwner string     `json:"nameWithOwner"`
      	URL           string     `json:"url"`
      	SSHURL        string     `json:"sshUrl"`
      	IsArchived    bool       `json:"isArchived"`
      	IsEmpty       bool       `json:"isEmpty"`
      	IsFork        bool       `json:"isFork"`
      	IsPrivate     bool       `json:"isPrivate"`
      	Visibility    string     `json:"visibility"`
      	PushedAt      time.Time  `json:"pushedAt"`
      	ArchivedAt    time.Time  `json:"archivedAt"`
      	DefaultBranch *ghRefName `json:"defaultBranchRef"`
      }

      type ghRefName struct {
      	Name string `json:"name"`
      }
      ```
- [ ] Add `ghJSONFields` as a package-level `const` string with exactly this value:
      `id,name,nameWithOwner,url,sshUrl,isArchived,archivedAt,isEmpty,isFork,isPrivate,visibility,pushedAt,defaultBranchRef`
- [ ] Add `listRepos(ctx context.Context, cfg config) ([]ghRepo, error)` invoking, through `runner`:
      `gh repo list <org> --limit <MaxRepos> --json <ghJSONFields>`
      Notes that must be respected: `--limit` is mandatory because **gh defaults to 30** and would
      silently truncate the org. Do **not** pass `--no-archived` (archived repos are the whole point of
      this tool) and do **not** pass `--source` to filter forks — forks are filtered client-side later so
      that `-v` can report why a repo was skipped.
- [ ] Add a `cloneURL(repo ghRepo, cfg config) string` helper returning `repo.SSHURL` when
      `cfg.Protocol == "ssh"` and `repo.URL` otherwise, and returning an error-free empty string never —
      if the chosen URL is empty, callers must treat it as a repo-level failure.
- [ ] Create `gh_test.go` with `TestParseRepoList`: unmarshal a canned payload that includes a normal
      repo, an archived repo with a non-null `archivedAt`, a fork, a private repo, and an empty repo with
      `"pushedAt": null, "archivedAt": null, "defaultBranchRef": null`. Assert the empty repo decodes
      with a zero `PushedAt` and a `nil` `DefaultBranch` and no error.
- [ ] Add `TestListReposArgs`: override `runner` (restoring it with `t.Cleanup`) to capture argv, call
      `listRepos`, and assert the argv is exactly `gh repo list <org> --limit <n> --json <fields>` with
      the full field list. This test exists specifically to catch a future edit that drops `--limit` and
      silently truncates large orgs.
- [ ] Add `TestListReposError`: `runner` returns a non-zero exit with stderr `HTTP 401: Requires
      authentication`; assert `listRepos` returns an error whose message contains that text. Surfacing
      gh's own stderr verbatim is the auth-failure UX — do not add a separate `gh auth status` preflight.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -run 'TestParseRepoList|TestListRepos' -v ./...`

---

## Stage 4 — Persisted state with atomic writes

- [ ] `jj new -m "feat: persisted state with atomic writes"`
- [ ] Create `state.go` with these types and constants. Note there is deliberately **no** `lastError`
      field: the invariant is that `PushedAt` is written only after a fully successful sync, so a failed
      repo is automatically eligible again on the next run. Record that invariant as an `AIDEV:` comment
      on `repoState`.
      ```go
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
      	Repos     map[string]repoState `json:"repos"` // key: repo name from gh
      }

      type repoState struct {
      	ID          string     `json:"id"`
      	PushedAt    time.Time  `json:"pushedAt"`
      	SyncedAt    time.Time  `json:"syncedAt"`
      	Status      repoStatus `json:"status"`
      	ArchivePath string     `json:"archivePath,omitempty"` // relative to <root>/<org>
      }
      ```
- [ ] Add path helpers to `state.go`, all derived from `cfg`: `orgDir` = `<Root>/<Org>`, `reposDir` =
      `<orgDir>/repos`, `archivesDir` = `<orgDir>/archives`, `statePath` = `<orgDir>/state.json`,
      `lockPath` = `<orgDir>/lock`. The `repos/` and `archives/` subdirectories are required, not
      cosmetic: `archives` and `state.json` are themselves legal GitHub repo names and would collide if
      clones lived directly under `<orgDir>`.
- [ ] Add `loadState(path, org string, stderr io.Writer) state`. It returns a usable state and never an
      error: a missing file yields an empty state; a file that fails to parse, or whose `Version !=
      stateVersion`, prints a warning to `stderr` and yields an empty state. Losing the cache costs one
      full re-verify pass and destroys nothing, so failing the run here would be strictly worse. Always
      return a non-nil `Repos` map.
- [ ] Add `saveState(path string, s state) error` writing atomically: marshal to memory with
      `json.MarshalIndent`, `os.CreateTemp(filepath.Dir(path), "state-*.json")`, `Write`, `Sync`,
      `Close`, `os.Rename` onto `path`, then open the parent directory and `Sync` that handle so the
      rename itself is durable. `defer os.Remove(tmp)` on every error path so a failure leaves no
      litter. Rename within one directory is atomic, so a concurrent reader sees either the whole old
      file or the whole new one, never a truncated one.
- [ ] Add `validRepoName(name string) bool` to `state.go`. This is a **trust boundary** — the name comes
      from the GitHub API and becomes a path segment and a `git` argument. Return true only for names
      matching `^[A-Za-z0-9][A-Za-z0-9._-]*$` that are additionally not `.` and not `..`. The anchored
      first character is what blocks a leading `-` being read by `git` as a flag.
- [ ] Create `state_test.go` with `TestStateRoundTrip`: save a state with two repos, load it, assert
      field-for-field equality including timestamps, assert timestamps serialise as RFC3339, and assert
      no `state-*.json` temp file remains in the directory afterwards.
- [ ] Add `TestLoadStateCorrupt`: a table over (a) missing file, (b) `not json at all`, (c) valid JSON
      with `"version": 99`, (d) valid JSON with `"repos": null`. Each must yield an empty, usable state
      with a non-nil map, no panic, and — for (b) and (c) — a warning written to the provided writer.
- [ ] Add `TestValidRepoName`: a table covering accepted (`repo`, `.github`, `foo.bar`, `a-b`, `x_y`,
      `a1`) and rejected (`""`, `.`, `..`, `a/b`, `-x`, `--upload-pack=x`, `.hidden-ok?` decide per the
      regex, `é`, `a b`) inputs.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -run 'TestState|TestLoadState|TestValidRepoName' -v ./...`

---

## Stage 5 — Pure per-repo action planner

- [ ] `jj new -m "feat: pure per-repo action planner"`
- [ ] Create `plan.go` with the action enum:
      ```go
      type action string

      const (
      	actionSkip          action = "skip"
      	actionClone         action = "clone"
      	actionFetch         action = "fetch"
      	actionArchive       action = "archive"
      	actionAdoptArchived action = "adopt-archived" // archive already on disk, just record it
      	actionUnarchive     action = "unarchive"     // was archived locally, now live upstream
      	actionNotARepo      action = "not-a-repo"    // dir exists, no .git — report, touch nothing
      )
      ```
- [ ] Add the planner to `plan.go`. It must be **pure**: no filesystem, no subprocess, no clock. All
      filesystem facts arrive as parameters so the whole decision matrix is table-testable.
      ```go
      func decide(repo ghRepo, prev repoState, known, dirExists, isGitDir, manifestExists bool, cfg config) (action, string)
      ```
      The returned string is a human-readable reason for `-v` output.
- [ ] Implement `decide` with this precedence, top to bottom:
      1. `dirExists && !isGitDir` → `actionNotARepo`.
      2. `repo.IsArchived && cfg.Archive`: `manifestExists && !dirExists` → `actionAdoptArchived`;
         otherwise → `actionArchive`.
      3. `!repo.IsArchived && known && prev.Status == statusArchived` → `actionUnarchive`.
      4. **Skip rule** — `known && !cfg.Force && prev.PushedAt.Equal(repo.PushedAt) && dirExists &&
         isGitDir && prev.Status == statusCloned` → `actionSkip`.
      5. `!dirExists` → `actionClone`.
      6. otherwise → `actionFetch`.
      Note that step 2 ignores `pushedAt` on purpose: an archived repo must be verified as actually
      archived on disk regardless of the cache.
- [ ] Add an `AIDEV:` comment on the skip rule recording its ceiling: `pushedAt` does not move for every
      conceivable upstream ref change, so `-force` is the escape hatch; the upgrade path is a periodic
      full `ls-remote` verification pass.
- [ ] Note that `isGitDir` must be computed by the caller as a single `os.Stat` of `<dir>/.git`, **not**
      by shelling out to `git rev-parse`. This is what makes a fully up-to-date run spawn zero
      subprocesses per repo — one `gh` call for the entire org.
- [ ] Create `plan_test.go` with `TestDecide`: a table walking the full matrix — `known` × `pushedAt`
      equal/changed × `dirExists` × `isGitDir` × `repo.IsArchived` × `prev.Status` × `cfg.Force` ×
      `cfg.Archive` × `manifestExists`. Assert the expected `action` for each row. This is the most
      important test in the suite; include at minimum these named rows: fresh unknown repo → clone;
      unchanged known repo → skip; unchanged known repo with `-force` → fetch; changed pushedAt →
      fetch; known repo whose directory was deleted → clone; directory present without `.git` →
      not-a-repo; newly archived upstream → archive; archived with manifest and no clone → adopt;
      archived upstream but `cfg.Archive == false` → fetch/clone as normal; previously archived, now
      live upstream → unarchive.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -run TestDecide -v ./...`

---

## Stage 6 — Clone, fetch and fast-forward git operations

- [ ] `jj new -m "feat: clone, fetch and fast-forward git operations"`
- [ ] Add `cloneRepo(ctx context.Context, cfg config, repo ghRepo) error` to `git.go`. Clone into a
      temp sibling and rename on success, so an interrupted clone can never be mistaken for a real one:
      ```
      git clone --quiet --no-single-branch --origin origin -- <url> <reposDir>/.tmp-<name>-<pid>
      ```
      then `os.Rename` the temp directory onto `<reposDir>/<name>`. On any failure `os.RemoveAll` the
      temp directory. Do **not** add `--depth`, `--filter=blob:none`, `--single-branch`, `--bare` or
      `--mirror`: this tool exists to give coding agents a readable offline working tree, and every one
      of those flags either removes the files or makes git fetch on demand.
- [ ] Add `fetchRepo(ctx context.Context, cfg config, dir string) error` running:
      `git -C <dir> fetch --quiet --all --tags --prune --prune-tags`
      `--prune --prune-tags` is what makes the local copy a true mirror of upstream refs, so deleted
      branches and tags disappear locally too.
- [ ] Add `isDirty(ctx context.Context, cfg config, dir string) (bool, error)` running
      `git -C <dir> status --porcelain` and reporting dirty when the output is non-empty.
- [ ] Add `updateWorktree(ctx context.Context, cfg config, dir, defaultBranch string) (string, error)`
      returning a warning string (empty when it fast-forwarded cleanly). It must **never** run `git
      reset --hard` or `git checkout -f`. Sequence: bail with a warning if `isDirty`; read the current
      branch with `git -C <dir> symbolic-ref --quiet --short HEAD` and bail with a warning if that fails
      (detached HEAD) or does not equal `defaultBranch`; then run
      `git -C <dir> merge --ff-only --quiet refs/remotes/origin/<defaultBranch>` and return its failure
      as a warning, not an error — a diverged local branch is the user's business, not a sync failure.
      Skip entirely when `defaultBranch == ""` (empty repo).
- [ ] Add an `AIDEV:` comment on `updateWorktree` naming its ceiling: a repo whose default branch was
      renamed upstream keeps its old checkout until a human runs `git switch`; the upgrade path is a
      rename-aware branch switch.
- [ ] Add `headInfo(ctx context.Context, cfg config, dir, defaultBranch string) (sha string, committedAt
      time.Time, subject string, err error)`. Resolve the commit with
      `git -C <dir> rev-parse --verify --quiet refs/remotes/origin/<defaultBranch>^{commit}`, falling
      back to `HEAD`; then
      `git -C <dir> log -1 --format=%H%x00%cI%x00%s <sha>`. Split on NUL, not tab or space — commit
      subjects routinely contain both. A repo with no commits must return zero values and **no** error.
- [ ] Add `tags(ctx context.Context, cfg config, dir string) ([]archiveTag, error)` running
      `git -C <dir> for-each-ref --format=%(refname:short)%x00%(objectname)%x00%(creatordate:iso-strict) refs/tags`
      and parsing NUL-separated fields per line. Define the type in `git.go` for now:
      ```go
      type archiveTag struct {
      	Name      string    `json:"name"`
      	SHA       string    `json:"sha"`
      	CreatedAt time.Time `json:"createdAt"`
      }
      ```
      No tags must yield an empty slice and no error.
- [ ] Add `setRemoteURL(ctx context.Context, cfg config, dir, url string) error` running
      `git -C <dir> remote set-url origin -- <url>`, used by the rename fix-up in Stage 8.
- [ ] Create `git_test.go` with a helper that builds a real git repo in `t.TempDir()`: `git init -b
      main`, set `user.name`/`user.email` locally, write a file, commit. These tests use the **real**
      `git` binary over `file://` URLs and need no network and no `runner` override — reserve the seam
      for `gh`.
- [ ] Add `TestCloneThenFetch`: build an origin repo, `cloneRepo` from its `file://` path, add a second
      commit upstream, `fetchRepo`, `updateWorktree`, and assert the working-tree file now contains the
      new content. Also assert no `.tmp-*` directory remains in `reposDir`.
- [ ] Add `TestUpdateWorktreeDirty`: clone, make an uncommitted edit, add an upstream commit, fetch, then
      `updateWorktree` — assert it returns a non-empty warning, the uncommitted edit is still on disk
      byte-for-byte, and the working tree was not fast-forwarded.
- [ ] Add `TestUpdateWorktreeDetached`: clone, `git checkout --detach`, then `updateWorktree` — assert a
      warning and no error.
- [ ] Add `TestHeadInfoAndTags`: a repo with a commit whose subject contains a tab character, one
      lightweight tag and one annotated tag. Assert the subject round-trips intact, both tags are
      returned with correct SHAs and parseable timestamps. Add a sub-case for a repo created with `git
      init` and no commits, asserting `headInfo` returns zero values and a nil error.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -run 'TestClone|TestUpdateWorktree|TestHeadInfo' -v ./...`

---

## Stage 7 — Tarball archives with sidecar manifests

- [ ] `jj new -m "feat: tarball archives with sidecar manifests"`
- [ ] Create `archive.go` with the manifest type. Recording sha, timestamps **and** tags together is the
      answer to the open question in `CLAUDE.md` ("commit hash? Tags? Updated timestamp?") — all three
      cost one `git log` plus one `git for-each-ref`, so record all three.
      ```go
      const manifestVersion = 1

      type archiveManifest struct {
      	Version         int          `json:"version"`
      	Org             string       `json:"org"`
      	Repo            string       `json:"repo"`
      	NameWithOwner   string       `json:"nameWithOwner"`
      	URL             string       `json:"url"`
      	DefaultBranch   string       `json:"defaultBranch"`
      	HeadSHA         string       `json:"headSha"`
      	HeadCommittedAt time.Time    `json:"headCommittedAt"`
      	HeadSubject     string       `json:"headSubject"`
      	Tags            []archiveTag `json:"tags"`
      	PushedAt        time.Time    `json:"pushedAt"`   // from GitHub
      	ArchivedAt      time.Time    `json:"archivedAt"` // from GitHub
      	CapturedAt      time.Time    `json:"capturedAt"` // when this tarball was written
      	Tarball         string       `json:"tarball"`    // file name only
      	TarballBytes    int64        `json:"tarballBytes"`
      	TarballSHA256   string       `json:"tarballSha256"`
      }
      ```
- [ ] Add `writeTarball(dir, repoName, dest string) (bytes int64, sha256hex string, err error)` to
      `archive.go`. Write to `<dest>.tmp-<pid>` first. Chain `os.Create` → `sha256.New()` →
      `io.MultiWriter(file, hash)` → `gzip.NewWriter` → `tar.NewWriter`, so the checksum costs nothing
      extra. Walk with `filepath.WalkDir` using `os.Lstat` (never `os.Stat`) so symlinks are stored as
      `tar.TypeSymlink` rather than followed — following them can escape the repo or loop. Skip sockets,
      devices and FIFOs with a warning. Include `.git` — it is the history and the integrity check.
      Header names are the path relative to `dir`, prefixed with `<repoName>/`, so extraction produces
      one directory instead of spraying files into the cwd. Close tar → close gzip → `file.Sync()` →
      `file.Close()` → `os.Rename` to `dest`. On any error, `os.Remove` the temp file and return.
- [ ] Add `archiveRepo(ctx context.Context, cfg config, repo ghRepo) (repoState, []string, error)`
      implementing this exact order. The ordering is the safety property: the clone is removed only
      after both files are durably on disk.
      1. If `<reposDir>/<name>` does not exist and `<archivesDir>/<name>.json` parses and its
         `.tar.gz` exists with a matching `TarballBytes` → return `statusArchived` and stop. The
         filesystem is the truth; state is only a cache.
      2. If the clone does not exist and there is no valid manifest → `cloneRepo` first. Archived repos
         are still readable on GitHub.
      3. `isDirty` → if dirty, **refuse to archive**: return an error, leave the tarball unwritten and
         the clone in place. Uncommitted work is never tarred-and-deleted. Add an `AIDEV:` comment
         saying there is deliberately no `-force` override for this.
      4. `fetchRepo` — the local copy may predate the last pushes before the repo was archived.
      5. Collect `headInfo` and `tags`. An empty repo yields an empty `HeadSHA` and no tags; that is not
         an error.
      6. **Resume check**: if `<archivesDir>/<name>.json` parses, its `HeadSHA` equals the sha from
         step 5, and the `.tar.gz` exists with matching size → skip to step 9. This is what makes a
         crashed run cheap to retry.
      7. `writeTarball` to `<archivesDir>/<name>.tar.gz`.
      8. Write the manifest to `<archivesDir>/<name>.json` via the same temp → `Sync` → `Close` →
         `os.Rename` dance. It must be written **after** the tarball, so "manifest exists" always
         implies "tarball complete".
      9. `os.Open(archivesDir)` → `Sync()` → `Close()`, flushing both renames' directory entries.
      10. `os.RemoveAll(<reposDir>/<name>)`.
      11. Return `repoState{ID: repo.ID, PushedAt: repo.PushedAt, SyncedAt: now, Status:
          statusArchived, ArchivePath: "archives/<name>.tar.gz"}`.
- [ ] Create `archive_test.go` with `TestArchiveRepo`: build a real repo containing a nested directory, a
      symlink and a file with mode `0o755`; run `archiveRepo`; then re-open the tarball with
      `archive/tar` and assert every entry name is prefixed `<repo>/`, the symlink is a
      `tar.TypeSymlink` entry (not its target's contents), the executable bit survived, and `.git`
      entries are present. Recompute sha256 over the file and assert it matches the manifest, assert
      `TarballBytes` matches the real size, and assert `<reposDir>/<name>` is gone.
- [ ] Add `TestArchiveRefusesDirty`: a clone with an uncommitted edit → `archiveRepo` returns an error,
      no `.tar.gz` and no `.json` exist, and the clone directory is still present with the edit intact.
- [ ] Add `TestArchiveResume`: pre-write a matching manifest and tarball, record the tarball's mtime,
      run `archiveRepo`, and assert the mtime is unchanged (no re-tar) while the clone is still deleted.
- [ ] Add `TestArchiveAdoptsExisting`: manifest and tarball present with no clone → returns
      `statusArchived` without invoking git at all.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -run TestArchive -v ./...`

---

## Stage 8 — Concurrent run orchestration

This is the largest stage. If it must be split, split it as worker-pool → signal handling → lock file,
in that order, each sub-step still compiling.

- [ ] `jj new -m "feat: concurrent run orchestration with signal-safe state"`
- [ ] Add the result type to `main.go`. Errors travel **in** this struct, never out of a worker, so one
      repo's failure can never abort another's work.
      ```go
      type result struct {
      	Name        string
      	Action      action
      	PushedAt    time.Time
      	Status      repoStatus
      	ArchivePath string
      	Notes       []string
      	Err         error
      }
      ```
- [ ] In `run`, after config resolution, preflight with `exec.LookPath("gh")` and `exec.LookPath("git")`.
      A missing binary is an actionable error naming the tool and `return 1`. Do **not** add a `gh auth
      status` preflight — `gh repo list` already fails in under a second and its stderr is the better
      message.
- [ ] Wrap the context with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`.
- [ ] `os.MkdirAll` the org dir, `repos/` and `archives/` with mode `0o700`. This tree can hold private
      source code, so it must not be world- or group-readable.
- [ ] Acquire a per-org lock: `os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)`, write
      the pid and start time into it, and `defer os.Remove(lockPath)`. If it already exists, fail with an
      error naming the file and telling the user to delete it if the previous run died. Two concurrent
      runs of the same org would otherwise interleave clones and last-write-wins the state file.
- [ ] Sweep leftovers from a previous interrupted run: remove every `<reposDir>/.tmp-*` directory. These
      are ours by construction and can never contain user data.
- [ ] Call `listRepos`. On failure, print the error (which carries gh's stderr) and `return 1`. Never
      continue with a partial listing: a truncated listing is indistinguishable from every repo having
      been deleted upstream.
- [ ] `loadState`, then run a **sequential** pre-pass — single-threaded, before any goroutine starts —
      doing all of the following:
      - Drop forks when `!cfg.IncludeForks`, reporting each under `-v`.
      - Drop repos failing `validRepoName`, with an error for each.
      - Detect case-insensitive name collisions with a lowercase-keyed map. An org holding both `Foo`
        and `foo` maps to one directory on APFS; skip **both** sides with an error rather than
        interleaving two repos into one clone. This is macOS-specific and real.
      - Rename fix-up: index the loaded state by `repoState.ID`. If a listed repo's ID matches a state
        entry stored under a different name, and the old directory exists while the new one does not,
        `os.Rename` the directory, move the state map key, and `setRemoteURL` to the new clone URL.
        Without this a rename costs an orphaned directory plus a full re-clone.
      - For each surviving repo, `os.Stat` `<reposDir>/<name>` and `<reposDir>/<name>/.git` and
        `<archivesDir>/<name>.json`, then call `decide`.
- [ ] If `cfg.DryRun`, print each repo's planned action and reason to `stdout` and `return 0` here,
      before any subprocess runs.
- [ ] Start `cfg.Concurrency` worker goroutines reading tasks from a buffered channel and writing
      `result` values to a results channel. Each worker derives a per-command timeout from `cfg.Timeout`.
      Dispatch by action: `actionClone` → clone then `updateWorktree`; `actionFetch` → fetch then
      `updateWorktree`; `actionArchive`/`actionAdoptArchived` → `archiveRepo`; `actionUnarchive` →
      clone into `repos/` and set `statusCloned`, leaving the old tarball and manifest **in place** with
      a warning that they are now stale (deleting them could destroy the only copy of history that has
      since been force-pushed); `actionSkip` → no subprocess at all; `actionNotARepo` → a report-only
      failure that touches nothing.
- [ ] Collect results in a **single** collector goroutine (or the main goroutine) after closing the task
      channel, and mutate `state.Repos` only there. No mutex, no `sync.Map` — race-free by construction,
      and `-race` proves it. Set `PushedAt` on a repo **only** when `result.Err == nil`; that single rule
      is what makes a failed repo automatically eligible next run with no retry bookkeeping.
- [ ] For repos present in state but absent from the listing: report them and **never delete anything**.
      Absence is indistinguishable from the token losing access to a private repo, so deleting would be
      data loss. Add an `AIDEV:` comment naming a future `-prune` flag as the upgrade path.
- [ ] Deliberately do **not** record `PushedAt` for a repo whose working tree was dirty, so the warning
      repeats every run instead of silently serving a stale tree forever. Add an `AIDEV:` comment
      accepting the cost of one wasted fetch per run for such a repo.
- [ ] `saveState` once, after collection finishes — **including** when the context was cancelled, so a
      Ctrl-C run banks every repo that did complete. Return `130` on cancellation.
- [ ] Print a summary line to `stdout`: `cloned=N fetched=N archived=N skipped=N failed=N`. Return `1`
      if `failed > 0`, else `0`.
- [ ] Verify: `gofmt -l . && go vet ./... && go build ./... && go test -race ./...`

---

## Stage 9 — Offline end-to-end test

- [ ] `jj new -m "test: offline end-to-end run over local repos"`
- [ ] Add `TestRunEndToEnd` to `main_test.go`. Build three real git repos in `t.TempDir()`: one normal
      with two commits, one to be reported archived, one created with `git init` and no commits.
      Override `runner` so that **only** `gh` calls are intercepted — return canned JSON whose `sshUrl`
      and `url` are `file://` paths to those repos, with `isArchived: true` on the second and
      `"pushedAt": null, "defaultBranchRef": null` on the third. Let every `git` call through to the
      real binary. Restore `runner` with `t.Cleanup`.
- [ ] Call `run(ctx, []string{"-root", tmpRoot, "-protocol", "https", "testorg"}, &stdout, &stderr)` and
      assert: exit code `0`; `<root>/testorg/repos/<normal>` exists with the expected file content;
      `<root>/testorg/repos/<archived>` does **not** exist; `<root>/testorg/archives/<archived>.tar.gz`
      and `.json` both exist; the empty repo is present and caused no error; `state.json` parses with
      `Version == stateVersion` and the right per-repo `Status` values; and the summary line on stdout
      reports the expected counts.
- [ ] Add the incremental assertion — this is the test for the core requirement that repeat runs do as
      little work as possible. Wrap `runner` in a counter that records the invoked binary. Run `run` a
      second time with identical arguments and assert **zero** `git` invocations occurred and exactly
      one `gh` invocation, and that the summary reports every live repo as skipped. The already-archived
      repo must be adopted from its manifest without invoking git.
- [ ] Add `TestRunNoGh`: point `PATH` at an empty directory via `t.Setenv` so `exec.LookPath("gh")`
      fails, and assert exit code `1` with an error message naming `gh`.
- [ ] Add `TestRunLockHeld`: pre-create `<root>/testorg/lock`, then assert `run` exits non-zero with an
      error naming the lock file path, and that it did not modify anything under `repos/`.
- [ ] Add `TestRunDryRun`: assert `-dry-run` prints a planned action per repo, exits `0`, and creates no
      clone directory and no `state.json`.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -race -run TestRun -v ./... && go test -race ./...`

---

## Stage 10 — README and live smoke test

- [ ] `jj new -m "docs: README and dry-run smoke test"`
- [ ] Write `README.md` covering: what the tool does; install (`go build` or `go install`); usage
      `gh-org-clone [flags] <org>`; the full flag/env/config-key table with defaults; the JSON config
      file format and its search path; and the on-disk layout, pointing coding agents at
      `<root>/<org>/repos`.
- [ ] Document these three things in the README explicitly, because each is a surprise otherwise:
      **disk footprint** — full-history working-tree clones of an entire org plus a tarball per archived
      repo, so a 500-repo org can be tens of gigabytes and there is no size cap; **the default root is
      `~/.local/share/gh-org-clone`**, which is not where a Mac user looks and which Spotlight and Time
      Machine will index and back up, so `-root` deserves top billing; and **the dirty-tree policy** —
      a repo with uncommitted changes is never updated and never archived, and re-warns every run.
- [ ] Note in the README that because the binary is named `gh-org-clone`, putting it on `PATH` makes
      `gh org-clone <org>` work as a `gh` extension for free. Do not add an extension manifest or any
      other scaffolding for this.
- [ ] Live smoke test (needs Stage 0's `gh auth login`): `./gh-org-clone -dry-run -v <org>`, then a real
      run, then an immediate second run. The second run must report every repo as skipped and issue no
      git subprocesses.
- [ ] Cross-check listing completeness against a large org:
      `./gh-org-clone -dry-run <org> | wc -l` versus
      `gh api /orgs/<org> --jq '.public_repos + .total_private_repos'`. If `gh repo list` turns out to
      truncate, the documented fallback is `gh api --paginate '/orgs/<org>/repos?per_page=100&type=all'`
      with a second struct mapping `pushed_at`, `archived`, `default_branch`, `ssh_url` and `clone_url`.
      Do not build that fallback unless this check proves it is needed.
- [ ] Verify: `gofmt -l . && go vet ./... && go test -race ./...`

---

## Deferred — consciously out of scope

Do not implement these without being asked. They are listed so a future reader knows they were decided,
not overlooked.

- **Pruning repos deleted upstream.** Absence from the listing is indistinguishable from lost access, so
  the tool reports and never deletes. A `-prune` flag is the upgrade path.
- **Submodules.** Not initialized; no `--recurse-submodules`. Each submodule may be private, external or
  cyclic, and is a repo in its own right.
- **Shallow, partial or bare clones.** Rejected: they defeat offline reference for coding agents.
- **zstd tarballs.** `compress/gzip` is stdlib; zstd would be the project's first dependency.
- **A size cap or disk quota.** Documented in the README instead.
- **Periodic full re-verification** beyond `-force`, and per-repo retry/backoff bookkeeping — the
  "`pushedAt` is written only on success" invariant covers retries.
- **Issues, PRs, wikis, releases and other non-git org data.** This tool mirrors git repositories.
