#!/usr/bin/env bash

# Stop at the first failed build: otherwise the script (and the image build
# that runs it) succeeds with a binary missing.
set -euo pipefail

# -trimpath and -s -w drop file system paths, the symbol table and DWARF:
# binaries are several times smaller, and panics still print function names
# and lines.
BUILD_FLAGS=(-trimpath -ldflags="-s -w")

PWD=`pwd`
TEAMGRAMAPP=${PWD}"/app"
INSTALL=${PWD}"/teamgramd"

echo "build idgen ..."
cd ${TEAMGRAMAPP}/service/idgen/cmd/idgen
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/idgen

echo "build status ..."
cd ${TEAMGRAMAPP}/service/status/cmd/status
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/status

echo "build dfs ..."
cd ${TEAMGRAMAPP}/service/dfs/cmd/dfs
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/dfs

echo "build media ..."
cd ${TEAMGRAMAPP}/service/media/cmd/media
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/media

echo "build authsession ..."
cd ${TEAMGRAMAPP}/service/authsession/cmd/authsession
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/authsession

echo "build biz ..."
cd ${TEAMGRAMAPP}/service/biz/biz/cmd/biz
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/biz

echo "build msg ..."
cd ${TEAMGRAMAPP}/messenger/msg/cmd/msg
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/msg

echo "build sync ..."
cd ${TEAMGRAMAPP}/messenger/sync/cmd/sync
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/sync

echo "build bff ..."
cd ${TEAMGRAMAPP}/bff/bff/cmd/bff
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/bff

echo "build session ..."
cd ${TEAMGRAMAPP}/interface/session/cmd/session
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/session

echo "build gnetway ..."
cd ${TEAMGRAMAPP}/interface/gnetway/cmd/gnetway
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/gnetway

echo "build pluma-admin ..."
cd ${TEAMGRAMAPP}/../tools/pluma-admin
go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/pluma-admin

#echo "build httpserver ..."
#cd ${TEAMGRAMAPP}/interface/httpserver/cmd/httpserver
#go build "${BUILD_FLAGS[@]}" -o ${INSTALL}/bin/httpserver
