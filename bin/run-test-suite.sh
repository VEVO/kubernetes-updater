#!/bin/bash

set -ex

go test -race -v ./...
