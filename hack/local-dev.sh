#!/bin/sh -eu
HACKDIR="$(dirname "$0")"
"$HACKDIR/add-cvo-overrides.sh"
oc scale --replicas=0 deploy/cluster-image-registry-operator -n openshift-image-registry
ENV=$(
    oc -n openshift-image-registry get deploy cluster-image-registry-operator -o json |
    jq -r '
        .spec.template.spec.containers[0].env[] |
        if .valueFrom.fieldRef.fieldPath == "metadata.namespace" then del(.valueFrom) | .value = "openshift-image-registry"
        elif .valueFrom.fieldRef.fieldPath == "metadata.name" then del(.valueFrom) | .value = "cluster-image-registry-operator"
        else . end |
        "\(.name)=\(.value)"
    '
)
"$HACKDIR/build/build.sh"
exec env $ENV "$HACKDIR/../tmp/_output/bin/cluster-image-registry-operator" --kubeconfig="${KUBECONFIG:-$HOME/.kube/config}"
