#!/usr/bin/env bash
# Regenerates channel.tl.pb.go from channel.tl.proto with the generator
# versions of its header. The gRPC code and the TL codec
# (codec_schema.tl.pb.go) are kept by hand.
set -euo pipefail
cd "$(dirname "$0")"
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
GOBIN=$bin go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
PATH=$bin:$PATH protoc -I=. -I=../../../../../third_party/teamgram-proto/mtproto \
	--go_out=. --go_opt=paths=source_relative channel.tl.proto
