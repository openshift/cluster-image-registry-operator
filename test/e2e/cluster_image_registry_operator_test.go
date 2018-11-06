package e2e_test

import (
	"testing"

	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

func TestClusterImageRegistryOperator(t *testing.T) {
	cr := &imageregistryapi.ImageRegistry{
		TypeMeta: metaapi.TypeMeta{
			Kind:       "ImageRegistry",
			APIVersion: imageregistryapi.SchemeGroupVersion.String(),
		},
		ObjectMeta: metaapi.ObjectMeta{
			Name:      "image-registry",
			Namespace: "openshift-image-registry",
		},
	}
	if err := sdk.Get(cr); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

}
