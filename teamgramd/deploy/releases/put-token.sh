#!/usr/bin/env bash
# Makes the upload token of the releases service on the server, if there is
# none, and sets it as the secret RELEASES_UPLOAD_TOKEN of the environment
# "release" of each client repository, which CI uploads builds with
# (docs/ops/releases.md). The token is never printed or passed on a command
# line.
#
#   put-token.sh <ssh host> [<owner/repo> ...]
set -euo pipefail
[ $# -ge 1 ] || { echo "usage: $0 <ssh host> [<owner/repo> ...]" >&2; exit 2; }
host=$1
shift
repos=("$@")
[ ${#repos[@]} -gt 0 ] || repos=(sckraaa/pluma-android sckraaa/pluma-ios sckraaa/pluma-desktop)
DIR=${PLUMA_RELEASES_SECRETS:-/opt/pluma/releases}

# The remote commands are built here, with $DIR expanded on this side.
# shellcheck disable=SC2029
ssh "$host" "install -d -m 0700 '$DIR' && { [ -s '$DIR/upload-token' ] || { umask 077 && openssl rand -hex 32 > '$DIR/upload-token.tmp' && chmod 0400 '$DIR/upload-token.tmp' && mv -f '$DIR/upload-token.tmp' '$DIR/upload-token'; }; }"
for repo in "${repos[@]}"; do
	# shellcheck disable=SC2029
	ssh "$host" "cat '$DIR/upload-token'" | gh secret set RELEASES_UPLOAD_TOKEN --env release -R "$repo"
	echo "put-token: RELEASES_UPLOAD_TOKEN set in $repo (release)"
done
