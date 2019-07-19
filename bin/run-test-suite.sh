#!/bin/bash

set -ex

go test -mod readonly -race -v ./...
