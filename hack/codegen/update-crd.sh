#!/bin/sh
set -eu

go run vendor/github.com/openshift/library-go/cmd/crd-schema-gen/main.go --domain operator.openshift.io --apis-dir pkg/apis --manifests-dir manifests/ "$@"
