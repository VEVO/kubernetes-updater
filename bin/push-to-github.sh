#!/bin/bash

export PATH=${PATH}:${GOPATH}/bin

if ! type -P github-release >/dev/null 2>&1 ; then
  go install github.com/aktau/github-release
fi

echo "Tag is ${TAG}"

github-release release \
    --user VEVO \
    --repo kubernetes-updater \
    --tag ${TAG}

github-release upload \
    --user VEVO \
    --repo kubernetes-updater \
    --tag ${TAG} \
    --name "roller" \
    --file roller
