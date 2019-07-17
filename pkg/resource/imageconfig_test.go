package resource

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestGetServiceHostnames(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry-service",
			Namespace: "image-registry",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       443,
					TargetPort: intstr.FromInt(5000),
				},
				{
					Port:       5000,
					TargetPort: intstr.FromInt(5000),
				},
			},
		},
	}
	expectedHosts := []string{}
	for _, svcPort := range svc.Spec.Ports {
		expectedPort := ""
		if svcPort.Port != 443 {
			expectedPort = fmt.Sprintf(":%d", svcPort.Port)
		}
		expectedHosts = append(expectedHosts, fmt.Sprintf("%s.%s.svc%s", svc.Name, svc.Namespace, expectedPort),
			fmt.Sprintf("%s.%s.svc.cluster.local%s", svc.Name, svc.Namespace, expectedPort))
	}

	fakeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	fakeIndexer.Add(svc)
	fakeLister := corelisters.NewServiceLister(fakeIndexer)
	hostnames, err := getServiceHostnames(fakeLister.Services("image-registry"), "image-registry-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(expectedHosts, hostnames) {
		t.Errorf("expected hostnames %v, got %v", expectedHosts, hostnames)
	}

}
