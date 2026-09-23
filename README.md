# gh-org-clone

Mirrors every repository in a GitHub organization into one local directory, so the whole org is
available offline as reference material (for coding agents or otherwise). On each run it clones what's
missing, fetches what already exists, and tarballs anything archived upstream — doing as little work as
possible on repeat runs.

## Install

```sh
go build -o gh-org-clone .
```

or `go install github.com/swanysimon/gh-org-clone@latest`.

Because the binary is named `gh-org-clone`, putting it on `PATH` also makes it work as a `gh` extension
for free: `gh org-clone <org>` runs it. No extension manifest is needed for this.

## Usage

```sh
gh-org-clone [flags] <org>
```

Requires `gh` (authenticated) and `git` on `PATH`.

## Flags, environment variables and config file keys

Precedence is **flags > environment > config file > defaults**.

Flags follow `gh`'s own convention: every long flag takes `--name` (Go's flag parser also accepts a
single dash, e.g. `-root`, but `--root` is how it's documented and how `gh --help` shows its own
flags); `-v`/`--verbose` is the one flag with a one-letter shorthand, again matching `gh`.

| Flag | Env var | Config key | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--root` | `GH_ORG_CLONE_ROOT` | `root` | see below | root directory for all cloned orgs |
| `--concurrency` | `GH_ORG_CLONE_CONCURRENCY` | `concurrency` | `8` | repos synced in parallel |
| `--timeout` | `GH_ORG_CLONE_TIMEOUT` | `timeout` | `30m` | per-subprocess timeout |
| `--max-repos` | `GH_ORG_CLONE_MAX_REPOS` | `maxRepos` | `10000` | `gh repo list --limit`; gh itself defaults to 30 |
| `--protocol` | `GH_ORG_CLONE_PROTOCOL` | `protocol` | `ssh` | `ssh` or `https` clone URLs |
| `--include-forks` | `GH_ORG_CLONE_INCLUDE_FORKS` | `includeForks` | `false` | include forked repos |
| `--archive` | `GH_ORG_CLONE_ARCHIVE` | `archive` | `true` | tarball archived repos and remove their clones |
| `--force` | — | — | `false` | ignore stored `pushedAt`, re-sync every repo |
| `--dry-run` | — | — | `false` | print planned actions, do nothing |
| `-v`, `--verbose` | — | — | `false` | verbose output (e.g. reports skipped forks) |
| `--config` | `GH_ORG_CLONE_CONFIG` | — | see below | path to the JSON config file |

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

## On-disk layout

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

## Verifying a large org wasn't silently truncated

`gh repo list` is always called with an explicit `--limit`, but if you want to double check nothing was
cut off:

```sh
./gh-org-clone --dry-run <org> | wc -l
gh api /orgs/<org> --jq '.public_repos + .total_private_repos'
```

If those numbers disagree, the fallback is `gh api --paginate '/orgs/<org>/repos?per_page=100&type=all'`.
