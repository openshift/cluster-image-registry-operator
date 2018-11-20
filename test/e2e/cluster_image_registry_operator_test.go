package e2e_test

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	imageregistryset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned/typed/imageregistry/v1alpha1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func TestClusterImageRegistryOperator(t *testing.T) {
	kubeconfig, err := client.GetConfig()
	if err != nil {
		t.Fatal(err)
	}

	client, err := imageregistryset.NewForConfig(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	err = wait.PollImmediate(1*time.Second, 10*time.Minute, func() (bool, error) {
		_, err := client.ImageRegistries().Get("image-registry", metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Errorf("error waiting for registry resource to appear: %v", err)
	}
}
