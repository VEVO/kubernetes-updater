#!/bin/bash

set -ex

go get github.com/tools/godep && \
  godep restore -v
