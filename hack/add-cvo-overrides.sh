#!/bin/sh -eu

OVERRIDES=$(kubectl get clusterversion/version -o jsonpath='{.spec.overrides}')
if [ -z "$OVERRIDES" ]; then
    kubectl patch clusterversion version --type json -p '
    - op: replace
      path: /spec/overrides
      value: []
    '
fi

CURRENT=$(kubectl get clusterversion/version -o jsonpath='{.spec.overrides[?(@.name=="cluster-image-registry-operator")].name}')
if [ -z "$CURRENT" ]; then
    kubectl patch clusterversion version --type json -p '
    - op: add
      path: /spec/overrides/-
      value:
        group: apps
        kind: Deployment
        name: cluster-image-registry-operator
        namespace: openshift-image-registry
        unmanaged: true
    '
fi

CURRENT=$(kubectl get clusterversion/version -o jsonpath='{.spec.overrides[?(@.name=="configs.imageregistry.operator.openshift.io")].name}')
if [ -z "$CURRENT" ]; then
    kubectl patch clusterversion version --type json -p '
    - op: add
      path: /spec/overrides/-
      value:
        group: apiextensions.k8s.io
        kind: CustomResourceDefinition
        name: configs.imageregistry.operator.openshift.io
        namespace: ""
        unmanaged: true
    '
fi

CURRENT=$(kubectl get clusterversion/version -o jsonpath='{.spec.overrides[?(@.name=="imagepruners.imageregistry.operator.openshift.io")].name}')
if [ -z "$CURRENT" ]; then
    kubectl patch clusterversion version --type json -p '
    - op: add
      path: /spec/overrides/-
      value:
        group: apiextensions.k8s.io
        kind: CustomResourceDefinition
        name: imagepruners.imageregistry.operator.openshift.io
        namespace: ""
        unmanaged: true
    '
fi
