#!/bin/sh
set -e
apk add --update -t build-deps go git mercurial
mkdir -p /go/src/github.com/ekristen
cp -r /src /go/src/github.com/ekristen/queryable
cd /go/src/github.com/ekristen/queryable
export GOPATH=/go
go get
go build -ldflags "-X main.Version $1 -X main.Build $2" -o /bin/queryable
apk del --purge build-deps
rm -rf /go
rm -rf /var/cache/apk/*
