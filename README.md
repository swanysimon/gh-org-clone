# gh-org-clone

A [`gh`](https://cli.github.com/) extension that mirrors every repository in a GitHub organization into
one local directory, so the whole org is available offline as reference material (for coding agents or
otherwise). On each run it clones what's missing, fetches what already exists, and tarballs anything
archived upstream — doing as little work as possible on repeat runs.

## Install

```sh
gh extension install swanysimon/gh-org-clone
```

This downloads a prebuilt binary for your platform — no Go toolchain required. Requires `gh` itself to
be authenticated (`gh auth login`) and `git` on `PATH`.

To upgrade later:

```sh
gh extension upgrade swanysimon/gh-org-clone
```

<details>
<summary>Building from source instead</summary>

```sh
go build -o gh-org-clone .
gh extension install .
```

`gh extension install .` (run from this checkout) links the extension to the binary you just built,
which is useful while developing against a local change.
</details>

## Usage

```sh
gh org-clone [flags] <org>
```

Run it against any org you can see with `gh` — public repos need no special access, private ones need a
token with read access to them.

```sh
gh org-clone my-org               # first run: clones everything
gh org-clone my-org               # second run: only fetches what changed
gh org-clone --dry-run -v my-org  # see what would happen, do nothing
```

### Worktrees

Every cloned repo is a normal, non-bare working tree, so `git worktree` works on it as-is. The
`worktree` subcommand is a thin wrapper that resolves `<org>/<repo>` to that path (cloning first if
needed) instead of you having to remember `<root>/<org>/repos/<repo>` yourself; git does the rest.

```sh
# Clones my-org/my-repo first if it isn't already local, then adds a worktree
# for `some-branch` wherever you point it — e.g. alongside how you keep other
# working copies, such as ~/code/my-repo-some-branch.
gh org-clone worktree add my-org/my-repo some-branch ~/code/my-repo-some-branch

gh org-clone worktree list my-org/my-repo     # list one repo's worktrees
gh org-clone worktree list my-org             # list every repo's worktrees
gh org-clone worktree remove my-org/my-repo ~/code/my-repo-some-branch
gh org-clone worktree remove --force my-org/my-repo ~/code/my-repo-some-branch  # even if it's dirty
```

`worktree add` fetches the central clone first, so a branch pushed since the last sync is available.
An existing local or `origin/` branch is checked out as-is; a branch that exists nowhere is created
from `origin/<default branch>` (with no upstream set, so the first `git push -u` decides it). Relative
paths are resolved against your current directory. It takes the same per-org lock as a sync run, so it
refuses while one is in progress.

`worktree add` refuses (after cloning, so the clone still lands) if the repo is archived upstream —
archived repos aren't expected to get new work. If the repo is already archived locally, it refuses
without re-cloning and points at the tarball instead. `worktree add`/`remove`/`list` accept `--root`,
`--protocol`, `--timeout` and `--config`, same as the sync command; `--concurrency`,
`--max-repos`, `--include-forks` and `--archive` don't apply to a single repo and aren't accepted
(their environment variables and config keys are ignored).

## Configure

Precedence is **flags > environment > config file > defaults**.

| Flag | Env var | Config key | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--root` | `GH_ORG_CLONE_ROOT` | `root` | see [Where repositories end up](#where-repositories-end-up) | root directory for all cloned orgs |
| `--concurrency` | `GH_ORG_CLONE_CONCURRENCY` | `concurrency` | `8` | repos synced in parallel |
| `--timeout` | `GH_ORG_CLONE_TIMEOUT` | `timeout` | `30m` | per-subprocess timeout |
| `--max-repos` | `GH_ORG_CLONE_MAX_REPOS` | `maxRepos` | `10000` | `gh repo list --limit`; gh itself defaults to 30 |
| `--protocol` | `GH_ORG_CLONE_PROTOCOL` | `protocol` | `ssh` | `ssh` or `https` clone URLs |
| `--include-forks` | `GH_ORG_CLONE_INCLUDE_FORKS` | `includeForks` | `false` | include forked repos |
| `--archive` | `GH_ORG_CLONE_ARCHIVE` | `archive` | `true` | tarball archived repos and remove their clones |
| `--force` | — | — | `false` | ignore stored `pushedAt`, re-sync every repo |
| `--dry-run` | — | — | `false` | print planned actions, do nothing |
| `-v`, `--verbose` | — | — | `false` | verbose output (e.g. reports skipped forks) |
| `--yes` | — | — | `false` | don't prompt before removing worktrees to archive a repo they belong to |
| `--config` | `GH_ORG_CLONE_CONFIG` | — | see below | path to the JSON config file |

Flags follow `gh`'s own convention: every long flag is `--name`; `-v`/`--verbose` is the one flag with a
one-letter shorthand, again matching `gh`. Flags can go before or after positional arguments; `--` ends
flag parsing. Boolean flags take an explicit value to turn off a default, e.g. `--archive=false`.

The config file is JSON, e.g.:

```json
{
  "concurrency": 4,
  "protocol": "https",
  "maxRepos": 500
}
```

Its search path (first match wins): `--config` flag, `$GH_ORG_CLONE_CONFIG`,
`$XDG_CONFIG_HOME/gh-org-clone/config.json`, else `~/.config/gh-org-clone/config.json`. An unknown key
or a value that fails to parse is a hard error — it is never silently ignored.

## Where repositories end up

```
<root>/<org>/state.json
<root>/<org>/repos/<name>/          # normal working trees — point coding agents here
<root>/<org>/archives/<name>.tar.gz
<root>/<org>/archives/<name>.json
```

`<root>` defaults to `$XDG_DATA_HOME/gh-org-clone`, falling back to `~/.local/share/gh-org-clone`. This
is **not** where most Mac users look for things, and Spotlight and Time Machine will index and back up
everything under it — pass `--root` explicitly if that matters to you.

## Things worth knowing before you run this on a big org

- **Disk footprint.** Every live repo gets a full-history working-tree clone (not `--bare`/`--mirror`,
  not shallow), and every archived repo gets a full tarball before its clone is deleted. A 500-repo org
  can easily run to tens of gigabytes, and there is no size cap.
- **A dirty working tree is never touched.** If a clone has uncommitted changes, this tool still fetches
  refs but leaves the checkout alone, warns, and will keep warning on every future run instead of ever
  fast-forwarding or archiving over your local edits.
- **Nothing is ever deleted because it's missing upstream.** A repo that disappears from the org listing
  (renamed, deleted, or just no longer visible to your token) is reported, never removed locally —
  absence looks identical to lost access.
- **Archiving a repo with a live `git worktree` asks first.** Deleting a repo's clone out from under a
  linked worktree elsewhere would permanently break that worktree with no clean recovery, so a normal
  sync run stops and asks before removing any worktree to proceed with archiving — and, on an
  unattended run (no terminal on stdin), answers "no" and refuses to archive rather than hang or guess.
  Pass `--yes` to answer "yes" unattended once you're sure.

## Verifying a large org wasn't silently truncated

`gh repo list` is always called with an explicit `--limit` (`--max-repos`), and a run warns if the listing
comes back exactly that long, since that is the only sign of truncation gh gives. To double check by hand,
compare the dry-run plan (which prints one line per repo, forks included only with `--include-forks`)
against the org's own count:

```sh
gh org-clone --dry-run --include-forks <org> | wc -l
gh api /orgs/<org> --jq '.public_repos + .total_private_repos'
```

The two can also differ because of repos your token can't see, or repos skipped for an invalid or
case-colliding name (reported on stderr, and they fail the run). If they disagree for another reason, the
fallback is `gh api --paginate '/orgs/<org>/repos?per_page=100&type=all'`.
