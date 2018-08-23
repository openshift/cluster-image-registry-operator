#!/bin/sh -eu
cd "$(dirname "$0")/.."

CURRENT_CONTEXT=$(oc config current-context)
SYSTEM_ADMIN_CONTEXT=${CURRENT_CONTEXT%/*}/system:admin

operator-sdk build docker.io/openshift/cluster-image-registry-operator:latest

NAMESPACE=$(
    oc --context="$SYSTEM_ADMIN_CONTEXT" apply \
        -o go-template --template="{{.metadata.name}}" \
        -f ./deploy/namespace.yaml
)

oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f ./deploy/rbac.yaml
oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f ./deploy/crd.yaml
oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f ./deploy/cr.yaml
cat ./deploy/operator.yaml |
    sed 's/imagePullPolicy: Always/imagePullPolicy: Never/' |
    oc --context="$SYSTEM_ADMIN_CONTEXT" -n "$NAMESPACE" apply -f -
