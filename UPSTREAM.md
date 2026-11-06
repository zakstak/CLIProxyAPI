# Update the fork from upstream

From a clean checkout of `zakstak/CLIProxyAPI`, run:

```sh
./scripts/sync-upstream.sh
```

The command fetches the stable `main` branch from
`router-for-me/CLIProxyAPI` and merges it into your current branch. A normal
merge preserves our Codex fixes and commit history. The command adds the
`upstream` remote on a fresh clone and reuses it on later runs.

Use your existing feature branch while a PR is open. Once the fork's changes are
merged, prepare each upstream update on a new branch:

```sh
git switch main
git pull --ff-only origin main
git switch -c chore/upstream-$(date +%Y%m%d-%H%M%S)
./scripts/sync-upstream.sh
```

Review the merge and run the relevant checks before publishing. For the Codex
accounting patch, run the focused regressions and server build using the
[sandbox workflow](docs/codex-websocket-outcomes.md#validation-and-delivery).
Check upstream's release notes for changes that need broader tests.

Publish the update for review with GitHub CLI:

```sh
git push -u origin HEAD
gh pr create --repo zakstak/CLIProxyAPI --base main --fill
```

If Git reports conflicts, resolve them, stage the files, and run
`git merge --continue`. Run the checks before pushing. To cancel the update,
run `git merge --abort`.

The helper refuses detached HEADs, unfinished Git operations, and uncommitted
changes. It disables automatic stashing for the merge and protects ignored
files from being overwritten. It updates only the local checkout; publishing,
merging the PR, and deploying the gateway remain separate steps.
