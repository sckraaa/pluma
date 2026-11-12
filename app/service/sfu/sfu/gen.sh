#!/usr/bin/env bash
# Regenerates the Go code of sfu.proto with the generator versions the rest
# of the repository uses (see the headers of the other *.pb.go files).
set -euo pipefail
cd "$(dirname "$0")"
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
GOBIN=$bin go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.5
GOBIN=$bin go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
PATH=$bin:$PATH protoc --go_out=. --go_opt=paths=source_relative \
	--go-grpc_out=. --go-grpc_opt=paths=source_relative sfu.proto
