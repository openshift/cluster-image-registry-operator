#!/bin/sh
set -eu

go run ./vendor/github.com/openshift/crd-schema-gen/cmd/crd-schema-gen/ --apis-dir pkg/apis --manifests-dir manifests/ "$@"
