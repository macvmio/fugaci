#!/bin/bash -e

cd "$(dirname "$0")"

cd ..

GOARCH=arm64 GOOS=darwin go build -race ./cmd/fugaci
scp fugaci m1:/Users/tomek/bin/
