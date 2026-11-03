#!/bin/sh

SRC_DIR=.
DST_DIR=$GOPATH/src/

MTPROTO_PATH=$GOPATH/src/github.com/teamgram/proto/mtproto

protoc -I=$SRC_DIR:$MTPROTO_PATH --proto_path=$GOPATH/src:./ --go_out=$DST_DIR --go-grpc_out=require_unimplemented_servers=false:$DST_DIR $SRC_DIR/*.proto

gofmt -w *.go
