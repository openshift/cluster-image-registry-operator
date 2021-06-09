package client

import (
	kubeset "k8s.io/client-go/kubernetes"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	batchset "k8s.io/client-go/kubernetes/typed/batch/v1"
	jobset "k8s.io/client-go/kubernetes/typed/batch/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"

	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	regopset "github.com/openshift/client-go/imageregistry/clientset/versioned"
	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
)

type Clients struct {
	Kube   kubeset.Interface
	Route  routeset.RouteV1Interface
	Config configset.ConfigV1Interface
	RegOp  regopset.Interface
	Core   coreset.CoreV1Interface
	Apps   appsset.AppsV1Interface
	RBAC   rbacset.RbacV1Interface
	Batch  batchset.BatchV1Interface
	Job    jobset.BatchV1Interface
}
