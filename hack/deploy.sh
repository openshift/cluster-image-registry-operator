#!/bin/sh -eu
cd "$(dirname "$0")/.."

CURRENT_CONTEXT=$(oc config current-context)
SYSTEM_ADMIN_CONTEXT=${CURRENT_CONTEXT%/*}/system:admin

make build-devel-image

NAMESPACE=$(
    oc --context="$SYSTEM_ADMIN_CONTEXT" apply \
        -o go-template --template="{{.metadata.name}}" \
        -f ./deploy/01-namespace.yaml
)

oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f ./deploy/02-rbac.yaml
oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f ./deploy/00-crd.yaml
oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f ./deploy/cr.yaml
oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" delete --ignore-not-found deploy/cluster-image-registry-operator
cat ./deploy/03-operator.yaml |
    sed 's/imagePullPolicy: Always/imagePullPolicy: Never/' |
    oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f -
