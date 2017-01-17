#!/bin/bash

set -ex

declare -r binary_name="${BINARY_NAME:-roller}"

godep go build -v -o ${binary_name} .
