#!/bin/sh -eu

override() {
    local group="$1" kind="$2" namespace="$3" name="$4" current
    current=$(kubectl get clusterversion version -o go-template="{{range .spec.overrides}}{{if and (eq .group \"$group\") (eq .kind \"$kind\") (eq .namespace \"$namespace\") (eq .name \"$name\")}}{{.unmanaged}}{{end}}{{end}}")
    if [ -z "$current" ]; then
        kubectl patch clusterversion version --type json -p "
        - op: add
          path: /spec/overrides/-
          value:
            group: $group
            kind: $kind
            namespace: \"$namespace\"
            name: $name
            unmanaged: true
        "
    fi
}

OVERRIDES=$(kubectl get clusterversion/version -o jsonpath='{.spec.overrides}')
if [ -z "$OVERRIDES" ]; then
    kubectl patch clusterversion version --type json -p '
    - op: replace
      path: /spec/overrides
      value: []
    '
fi

override apps Deployment openshift-image-registry cluster-image-registry-operator
override apiextensions.k8s.io CustomResourceDefinition "" configs.imageregistry.operator.openshift.io
override apiextensions.k8s.io CustomResourceDefinition "" imagepruners.imageregistry.operator.openshift.io
override rbac.authorization.k8s.io ClusterRole "" cluster-image-registry-operator
override rbac.authorization.k8s.io Role openshift-image-registry cluster-image-registry-operator
