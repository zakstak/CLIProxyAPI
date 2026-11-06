#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: ./scripts/sync-upstream.sh

Fetch upstream/main and merge it into the current branch, preserving fork commits.
Requires a clean checkout. Review and test the result before pushing it.
EOF
}

if [[ $# -gt 0 ]]; then
	if [[ $# -eq 1 && ( $1 == --help || $1 == -h ) ]]; then
		usage
		exit 0
	fi
	usage >&2
	exit 2
fi

repo_dir="$(git -C "$(dirname -- "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
cd "$repo_dir"

if ! branch="$(git symbolic-ref --quiet --short HEAD)"; then
	printf 'Switch to a fork branch before syncing upstream; HEAD is detached.\n' >&2
	exit 1
fi

for state in MERGE_HEAD CHERRY_PICK_HEAD REVERT_HEAD rebase-merge rebase-apply sequencer; do
	if [[ -e $(git rev-parse --git-path "$state") ]]; then
		printf 'Finish or abort the existing Git operation before syncing upstream (%s).\n' "$state" >&2
		exit 1
	fi
done

if [[ -n $(git status --porcelain --untracked-files=normal) ]]; then
	printf 'Commit or stash your changes, including untracked files, before syncing upstream.\n' >&2
	exit 1
fi

if ! git remote get-url upstream >/dev/null 2>&1; then
	git remote add upstream https://github.com/router-for-me/CLIProxyAPI.git
fi

printf 'Fetching upstream/main from %s for branch %s.\n' "$(git remote get-url upstream)" "$branch"
git fetch --no-tags --no-recurse-submodules upstream \
	refs/heads/main:refs/remotes/upstream/main

# Git's merge strategy can overwrite ignored files despite --no-overwrite-ignore.
# Check new upstream paths and any file/symlink blocking their parent directories.
while IFS= read -r -d '' path; do
	candidate=$path
	while :; do
		if [[ $candidate == "$path" || -f $candidate || -L $candidate ]] &&
			[[ -n $(git ls-files --others --ignored --exclude-standard -- ":(literal)$candidate") ]]; then
			printf 'Move ignored local files out of %s before syncing upstream.\n' "$candidate" >&2
			exit 1
		fi
		[[ $candidate == */* ]] || break
		candidate=${candidate%/*}
	done
done < <(git diff --no-renames --name-only --diff-filter=A -z HEAD refs/remotes/upstream/main)

if ! git -c merge.autoStash=false merge --ff --no-edit --no-overwrite-ignore refs/remotes/upstream/main; then
	if [[ -f $(git rev-parse --git-path MERGE_HEAD) ]]; then
		printf '\nResolve the conflicts, stage the files, and run git merge --continue.\n' >&2
		printf 'To cancel this update, run git merge --abort.\n' >&2
	else
		printf '\nThe merge did not complete. Inspect git status and the error above.\n' >&2
	fi
	exit 1
fi

printf '\nBranch %s now includes upstream/main at %s.\n' \
	"$branch" "$(git rev-parse --short refs/remotes/upstream/main)"
printf 'Review the changes and run the relevant checks, then push with git push.\n'
