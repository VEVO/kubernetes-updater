#!/bin/bash

set -ex

godep go test -v \
  $(find . -maxdepth 1 -name "*.go" | grep -v roller.go)
