package client

import (
	kubeset "k8s.io/client-go/kubernetes"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	jobset "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchset "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"

	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	regopset "github.com/openshift/client-go/imageregistry/clientset/versioned"
	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
)

type Clients struct {
	Kube   *kubeset.Clientset
	Route  *routeset.RouteV1Client
	Config *configset.ConfigV1Client
	RegOp  *regopset.Clientset
	Core   *coreset.CoreV1Client
	Apps   *appsset.AppsV1Client
	RBAC   *rbacset.RbacV1Client
	Batch  *batchset.BatchV1beta1Client
	Job    *jobset.BatchV1Client
}
