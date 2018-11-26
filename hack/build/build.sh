#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

if ! which go > /dev/null; then
	echo "golang needs to be installed"
	exit 1
fi

BIN_DIR="$(pwd)/tmp/_output/bin"
mkdir -p ${BIN_DIR}
PROJECT_NAME="cluster-image-registry-operator"
REPO_PATH="github.com/openshift/cluster-image-registry-operator"
BUILD_PATH="${REPO_PATH}/cmd/${PROJECT_NAME}"
VERSION="$(git describe --tags --always --dirty)"
GO_LDFLAGS="-X ${REPO_PATH}/version.Version=${VERSION}"
echo "building ${PROJECT_NAME}..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ${BIN_DIR}/${PROJECT_NAME} -ldflags "${GO_LDFLAGS}" $BUILD_PATH
