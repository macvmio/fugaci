#!/bin/bash -e

cd "$(dirname "$0")"

cd ..

ssh_m1() {
  ssh m1 "$@"
}


BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
echo "Building ${BUILD_TIME}"
GOARCH=arm64 GOOS=darwin go build -ldflags="-X 'github.com/macvmio/fugaci/cmd/fugaci/cmd.BuildTime=${BUILD_TIME}'" ./cmd/fugaci


ssh_m1 sudo /Users/tomek/bin/fugaci daemon bootout
scp fugaci m1:/Users/tomek/bin/
ssh_m1 sudo /Users/tomek/bin/fugaci daemon bootstrap
