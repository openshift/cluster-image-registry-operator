package e2e_test

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	routeapiv1 "github.com/openshift/api/route/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestPodResourceConfiguration(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryv1.ImageRegistryResourceName,
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Filesystem: &imageregistryv1.ImageRegistryConfigStorageFilesystem{
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Replicas: 1,
			Resources: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"memory": resource.MustParse("512Mi"),
				},
			},
			NodeSelector: map[string]string{
				"node-role.kubernetes.io/master": "",
			},
		},
	}
	testframework.MustDeployImageRegistry(t, client, cr)
	testframework.MustEnsureImageRegistryIsAvailable(t, client)
	testframework.MustEnsureClusterOperatorStatusIsSet(t, client)

	pods, err := client.Pods(imageregistryv1.ImageRegistryOperatorNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pods.Items) == 0 {
		t.Errorf("no pods found in registry namespace")
	}

	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, "image-registry") {
			mem, ok := pod.Spec.Containers[0].Resources.Limits["memory"]
			if !ok {
				t.Errorf("no memory limit set on registry pod: %#v", pod)
			}
			if mem.String() != "512Mi" {
				t.Errorf("expected memory limit of 512Mi, found: %s", mem.String())
			}
		}
	}
}

func TestRouteConfiguration(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryv1.ImageRegistryResourceName,
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Filesystem: &imageregistryv1.ImageRegistryConfigStorageFilesystem{
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Replicas:     1,
			DefaultRoute: true,
			Routes: []imageregistryv1.ImageRegistryConfigRoute{
				{
					Name:     "testroute",
					Hostname: "test.example.com",
				},
			},
		},
	}
	testframework.MustDeployImageRegistry(t, client, cr)
	testframework.MustEnsureImageRegistryIsAvailable(t, client)
	testframework.MustEnsureClusterOperatorStatusIsSet(t, client)
	ensureExternalRegistryHostnamesAreSet(t, client)
	ensureExternalRoutesExist(t, client)

}

func ensureExternalRegistryHostnamesAreSet(t *testing.T, client *testframework.Clientset) {
	var cfg *configapiv1.Image
	var err error
	externalHosts := []string{}
	err = wait.Poll(1*time.Second, testframework.AsyncOperationTimeout, func() (bool, error) {
		var err error
		cfg, err = client.Images().Get("cluster", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			t.Logf("waiting for the image config resource: the resource does not exist")
			cfg = nil
			return false, nil
		} else if err != nil {
			return false, err
		}
		if cfg == nil {
			return false, nil
		}
		externalHosts = cfg.Status.ExternalRegistryHostnames

		foundDefaultRoute := false
		foundUserRoute := false
		for _, h := range externalHosts {
			if strings.HasPrefix(h, imageregistryv1.DefaultRouteName+"-"+imageregistryv1.ImageRegistryOperatorNamespace) {
				foundDefaultRoute = true
				continue
			}
			if h == "test.example.com" {
				foundUserRoute = true
				continue
			}
		}
		return foundDefaultRoute && foundUserRoute, nil
	})
	if err != nil {
		t.Errorf("cluster image config resource was not updated with default external registry hostname: %v, err: %v", externalHosts, err)
	}
}

func ensureExternalRoutesExist(t *testing.T, client *testframework.Clientset) {
	var err error
	var routes *routeapiv1.RouteList
	err = wait.Poll(1*time.Second, testframework.AsyncOperationTimeout, func() (bool, error) {
		routes, err = client.Routes(imageregistryv1.ImageRegistryOperatorNamespace).List(metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		if routes == nil || len(routes.Items) < 2 {
			t.Logf("insuffient routes found: %#v", routes)
			return false, nil
		}

		foundDefaultRoute := false
		foundUserRoute := false
		for _, r := range routes.Items {
			if strings.HasPrefix(r.Spec.Host, imageregistryv1.DefaultRouteName+"-"+imageregistryv1.ImageRegistryOperatorNamespace) {
				foundDefaultRoute = true
				continue
			}
			if r.Spec.Host == "test.example.com" {
				foundUserRoute = true
			}
		}
		return foundDefaultRoute && foundUserRoute, nil
	})
	if err != nil {
		t.Errorf("did not find expected routes: %#v, err: %v", routes, err)
	}
}
