package e2e_test

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

func TestClusterImageRegistryOperator(t *testing.T) {
	cr := &imageregistryapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ImageRegistry",
			APIVersion: imageregistryapi.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry",
			Namespace: "openshift-image-registry",
		},
	}
	err := wait.PollImmediate(1*time.Second, 10*time.Minute, func() (bool, error) {
		if err := sdk.Get(cr); err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Errorf("error waiting for registry resource to appear: %v", err)
	}
}
