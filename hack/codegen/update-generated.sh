#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Reset the pkg/generated directory so we don't end
# up with files that should have been removed
rm -rf pkg/generated

bash vendor/k8s.io/code-generator/generate-groups.sh \
deepcopy,client,lister,informer \
github.com/openshift/cluster-image-registry-operator/pkg/generated \
github.com/openshift/cluster-image-registry-operator/pkg/apis \
imageregistry:v1 \
--go-header-file "./hack/codegen/boilerplate.go.txt"
