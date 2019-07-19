#!/bin/bash

set -ex

declare -r binary_name="${BINARY_NAME:-roller}"

go build -mod readonly -v -o ${binary_name} .
