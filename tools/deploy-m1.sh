#!/bin/bash -e

cd "$(dirname "$0")"

cd ..

GOARCH=arm64 GOOS=darwin go build
scp fugaci m1:/Users/tomek/bin/