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
REPO_PATH="github.com/openshift/cluster-image-registry-operator"
VERSION="$(git describe --tags --always --dirty)"
GO_LDFLAGS="-X ${REPO_PATH}/pkg/version.Version=${VERSION}"

# Build cluster-image-registry-operator
PROJECT_NAME="cluster-image-registry-operator"
BUILD_PATH="${REPO_PATH}/cmd/${PROJECT_NAME}"
echo "building ${PROJECT_NAME}..."
go build -o ${BIN_DIR}/${PROJECT_NAME} -ldflags "${GO_LDFLAGS}" ${BUILD_PATH}

# Build cluster-image-registry-operator-tests-ext
PROJECT_NAME="cluster-image-registry-operator-tests-ext"
BUILD_PATH="${REPO_PATH}/cmd/${PROJECT_NAME}"
echo "building ${PROJECT_NAME}..."
go build -o ${BIN_DIR}/${PROJECT_NAME} -ldflags "${GO_LDFLAGS}" ${BUILD_PATH}
