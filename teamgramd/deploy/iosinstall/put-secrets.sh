#!/usr/bin/env bash
# Puts the install service's secrets on the server, from the operator's
# machine (docs/ops/ios-install.md). Nothing secret is printed or passed on
# a command line:
#   - the App Store Connect key (.p8), the Apple Distribution .p12 and its
#     password file go to $DIR on the server, owned by the service (uid
#     65532), mode 0400;
#   - an upload token is made on the server if there is none, and set as
#     the secret IOSINSTALL_UPLOAD_TOKEN of the environment "release" of the
#     iOS repository, which CI uploads builds with.
#
#   put-secrets.sh <ssh host> <key.p8> <distribution.p12> <p12 password file> [<iOS repository>]
set -euo pipefail
[ $# -ge 4 ] || { echo "usage: $0 <ssh host> <key.p8> <distribution.p12> <p12 password file> [<owner/repo>]" >&2; exit 2; }
host=$1 key=$2 p12=$3 pw=$4 repo=${5:-sckraaa/pluma-ios}
DIR=${PLUMA_IOSINSTALL_SECRETS:-/opt/pluma/iosinstall}
for f in "$key" "$p12" "$pw"; do [ -s "$f" ] || { echo "$f: missing or empty" >&2; exit 1; }; done

# The remote commands are built here, with $DIR expanded on this side.
# shellcheck disable=SC2029
# The service's uid has no name on the host: chown takes the number.
ssh "$host" "install -d -m 0700 '$DIR' && chown 65532:65532 '$DIR'"
put() { # put <local file> <name>: through stdin, never in arguments
	# shellcheck disable=SC2029
	ssh "$host" "umask 077 && cat > '$DIR/$2.tmp' && chown 65532:65532 '$DIR/$2.tmp' && chmod 0400 '$DIR/$2.tmp' && mv -f '$DIR/$2.tmp' '$DIR/$2'" <"$1"
}
put "$key" asc-key.p8
put "$p12" distribution.p12
put "$pw" distribution.p12.password
# shellcheck disable=SC2029
ssh "$host" "[ -s '$DIR/upload-token' ] || { umask 077 && openssl rand -hex 32 > '$DIR/upload-token' && chown 65532:65532 '$DIR/upload-token' && chmod 0400 '$DIR/upload-token'; }"
# shellcheck disable=SC2029
ssh "$host" "cat '$DIR/upload-token'" | gh secret set IOSINSTALL_UPLOAD_TOKEN --env release -R "$repo"
# shellcheck disable=SC2029
ssh "$host" "ls -l '$DIR' | awk 'NR>1 {print \$1, \$3, \$NF}'"
echo "put-secrets: done; IOSINSTALL_UPLOAD_TOKEN set in $repo (release)"
