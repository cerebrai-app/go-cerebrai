#!/usr/bin/env bash
# release.sh tags a new release of brainctl and workctl and pushes the tag,
# which starts the Release workflow (.github/workflows/release.yml).
#
# Usage:
#   ./release.sh patch|minor|major   bump the latest release, e.g. v0.5.0 -> v0.5.1
#   ./release.sh v1.2.3              release an exact version (v1.0.0-rc1 for a prerelease)
#   ./release.sh -n ...              dry run: check everything, but tag and push nothing
#
# It switches to main and fast-forwards it to origin/main, so you can run it
# from any branch with a clean working tree. Before tagging, it checks that the
# version is new and higher than every earlier one, and runs the checks from
# CLAUDE.md. See README.md, "Releasing".

set -euo pipefail

remote=origin
branch=main

die() {
	printf 'release.sh: %s\n' "$*" >&2
	exit 1
}

usage() {
	sed -n '5,8s/^# \{0,1\}//p' "$0" >&2
	exit 2
}

# semver_re matches a tag like v1.2.3 or v1.2.3-rc1, capturing major, minor,
# patch and the prerelease part.
semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'

# version_gt reports whether tag $1 is a higher version than tag $2. A release
# is higher than its own prereleases (v1.0.0 > v1.0.0-rc1).
version_gt() {
	[[ $1 =~ $semver_re ]] || return 1
	local a=("${BASH_REMATCH[@]:1:4}")
	[[ $2 =~ $semver_re ]] || return 1
	local b=("${BASH_REMATCH[@]:1:4}")
	local i
	for i in 0 1 2; do
		((a[i] > b[i])) && return 0
		((a[i] < b[i])) && return 1
	done
	# Same major.minor.patch: compare the prerelease parts.
	[[ ${a[3]} == "${b[3]}" ]] && return 1
	[[ -z ${a[3]} ]] && return 0
	[[ -z ${b[3]} ]] && return 1
	[[ $(printf '%s\n%s\n' "${a[3]}" "${b[3]}" | sort -V | tail -n1) == "${a[3]}" ]]
}

# main does the release. The whole script is in functions, so bash has read all
# of it before main runs: switching branches may change this file underneath it.
main() {
	dry_run=false
	if [[ ${1-} == -n || ${1-} == --dry-run ]]; then
		dry_run=true
		shift
	fi
	[[ $# -eq 1 ]] || usage
	arg=$1

	cd "$(dirname "$0")"

	[[ -z $(git status --porcelain) ]] ||
		die "the working tree has uncommitted changes; commit or stash them first"

	# Release exactly what is on GitHub, not a stale clone.
	git fetch --quiet --tags "$remote" "$branch" ||
		die "cannot fetch from $remote; check your network and credentials"

	current=$(git symbolic-ref --quiet --short HEAD || true)
	if [[ $current != "$branch" ]]; then
		echo "==> Switching from ${current:-a detached HEAD} to $branch"
		if git rev-parse --quiet --verify "refs/heads/$branch" >/dev/null; then
			git switch --quiet "$branch"
		else
			git switch --quiet --track -c "$branch" "$remote/$branch"
		fi
	fi
	if [[ $(git rev-parse HEAD) != "$(git rev-parse "$remote/$branch")" ]]; then
		git merge --quiet --ff-only "$remote/$branch" ||
			die "$branch has commits that are not on $remote/$branch; push them through a pull request, or reset $branch to $remote/$branch"
		echo "==> Updated $branch to $(git log -1 --format='%h %s')"
	fi

	# latest is the highest release tag; latest_any also counts prereleases.
	tags=()
	while IFS= read -r t; do
		[[ $t =~ $semver_re ]] && tags+=("$t")
	done < <(git tag --list 'v*')
	latest=""
	latest_any=""
	for t in "${tags[@]}"; do
		if [[ -z $latest_any ]] || version_gt "$t" "$latest_any"; then
			latest_any=$t
		fi
		if [[ ! $t =~ -[0-9A-Za-z.-]+$ ]] && { [[ -z $latest ]] || version_gt "$t" "$latest"; }; then
			latest=$t
		fi
	done

	case $arg in
	patch | minor | major)
		[[ $latest =~ $semver_re ]] || die "no earlier release to bump; give an exact version such as v0.1.0"
		major=${BASH_REMATCH[1]} minor=${BASH_REMATCH[2]} patch=${BASH_REMATCH[3]}
		case $arg in
		patch) version="v$major.$minor.$((patch + 1))" ;;
		minor) version="v$major.$((minor + 1)).0" ;;
		major) version="v$((major + 1)).0.0" ;;
		esac
		;;
	*)
		version=$arg
		[[ $version == v* ]] || version="v$version"
		[[ $version =~ $semver_re ]] ||
			die "$arg is not a version like v1.2.3 or v1.2.3-rc1, or one of patch, minor, major"
		;;
	esac

	if git rev-parse --quiet --verify "refs/tags/$version" >/dev/null ||
		[[ -n $(git ls-remote --tags "$remote" "refs/tags/$version") ]]; then
		die "$version already exists; pick another version"
	fi
	if [[ -n $latest_any ]] && ! version_gt "$version" "$latest_any"; then
		die "$version is not higher than the latest tag, $latest_any; pick a higher version"
	fi

	if [[ -n $(git tag --points-at HEAD --list 'v*') ]]; then
		die "HEAD is already tagged $(git tag --points-at HEAD --list 'v*' | tr '\n' ' ')- there is nothing new to release"
	fi

	echo "==> Checks"
	command -v go >/dev/null || die "go is not installed"
	unformatted=$(gofmt -l .)
	[[ -z $unformatted ]] || die "gofmt would change these files: $unformatted"
	go vet ./...
	go test -race ./...
	go mod tidy -diff >/dev/null || die "go.mod or go.sum is not tidy; run go mod tidy in a pull request"
	go run ./cmd/brainctl version >/dev/null
	if command -v goreleaser >/dev/null; then
		goreleaser check
	fi

	echo
	echo "Latest release: ${latest:-none}"
	[[ $latest_any != "$latest" ]] && echo "Latest tag:     $latest_any"
	echo "New release:    $version"
	[[ $version == *-* ]] && echo "                (prerelease: no Homebrew casks or Scoop manifests)"
	echo "Commit:         $(git log -1 --format='%h %s')"
	if [[ -n $latest ]]; then
		echo
		echo "Commits since $latest:"
		git log --format='  %h %s' "$latest..HEAD"
	fi
	echo

	if $dry_run; then
		echo "Dry run: not tagging. Run without -n to release $version."
		exit 0
	fi

	read -r -p "Tag $version and push it to $remote? [y/N] " answer
	[[ $answer == [yY] || $answer == [yY][eE][sS] ]] || die "cancelled; nothing was tagged"

	git tag -a "$version" -m "$version"
	if ! git push "$remote" "refs/tags/$version"; then
		git tag -d "$version" >/dev/null
		die "pushing $version failed; the local tag was removed, so you can run this again"
	fi

	url=$(git remote get-url "$remote" | sed -E 's#^git@github.com:#https://github.com/#; s#\.git$##')
	echo
	echo "Pushed $version. Follow the Release workflow at $url/actions/workflows/release.yml"
}

main "$@"
