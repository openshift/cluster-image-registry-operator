package resource

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func TestExpectedService(t *testing.T) {
	params := parameters.Globals{}

	params.Deployment.Namespace = "image-registry"
	params.Deployment.Labels = map[string]string{"docker-registry": "default"}

	params.Container.Port = 5000

	params.Service.Name = imageregistryv1.ImageRegistryName
	params.Service.Ports = []int{443, 5000}

	fakeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	fakeLister := corelisters.NewServiceLister(fakeIndexer)
	fakeClient := fake.NewSimpleClientset()

	generator := newGeneratorService(fakeLister.Services("image-registry"), fakeClient.CoreV1(), &params, nil)
	svcGenerated := generator.expected()
	if svcGenerated.Name != generator.GetName() {
		t.Errorf("expected service name to be %s, got %s", generator.GetName(), svcGenerated.Name)
	}
	if svcGenerated.Namespace != generator.GetNamespace() {
		t.Errorf("expected service namespace to be %s, got %s", generator.GetName(), svcGenerated.Name)
	}
	if !reflect.DeepEqual(svcGenerated.Labels, params.Deployment.Labels) {
		t.Errorf("expected service to have labels %v, got %v", params.Deployment.Labels, svcGenerated.Labels)
	}
	if !reflect.DeepEqual(svcGenerated.Spec.Selector, params.Deployment.Labels) {
		t.Errorf("expected service selector to be %v, got %v", params.Deployment.Labels, svcGenerated.Spec.Selector)
	}
	for i, svcPort := range params.Service.Ports {
		actualSvcPort := svcGenerated.Spec.Ports[i]
		if actualSvcPort.TargetPort != intstr.FromInt(params.Container.Port) {
			t.Errorf("expected port %s target port to be %d, got %s", actualSvcPort.Name, params.Container.Port, actualSvcPort.TargetPort.StrVal)
		}
		if actualSvcPort.Port != int32(svcPort) {
			t.Errorf("expected port %s to be %d, got %d", actualSvcPort.Name, svcPort, actualSvcPort.Port)
		}
		if actualSvcPort.Protocol != "TCP" {
			t.Errorf("expected port %s to use protocol %s, got %s", actualSvcPort.Name, "TCP", actualSvcPort.Protocol)
		}
	}
}
