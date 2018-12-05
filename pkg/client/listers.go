package client

import (
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	regoplisters "github.com/openshift/cluster-image-registry-operator/pkg/generated/listers/imageregistry/v1alpha1"
)

type Listers struct {
	Deployments     appslisters.DeploymentNamespaceLister
	Services        corelisters.ServiceNamespaceLister
	ImageRegistry   regoplisters.ImageRegistryLister
	OpenShiftConfig corelisters.ConfigMapNamespaceLister
}
