package imagestream

import (
	"fmt"
	"testing"

	"github.com/docker/distribution/context"
	"github.com/opencontainers/go-digest"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	core "k8s.io/client-go/testing"

	imageapiv1 "github.com/openshift/api/image/v1"
	imagefakeclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1/fake"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	"github.com/openshift/image-registry/pkg/testutil"
)

func TestCachedImageGetter(t *testing.T) {
	dgst := digest.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000001")
	dockerImageReference := "localhost:5000/random/string"

	ctx := context.Background()
	ctx = testutil.WithTestLogger(ctx, t)
	imageClient := &imagefakeclient.FakeImageV1{Fake: &core.Fake{}}

	imageGetter := newCachedImageGetter(client.NewFakeRegistryAPIClient(nil, imageClient))
	imageClient.AddReactor("get", "images", func(action core.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(action.GetResource().GroupResource(), action.GetResource().Resource)
	})
	_, err := imageGetter.Get(ctx, dgst)
	if err == nil {
		t.Fatal("got nil, want error")
	}

	imageClient.PrependReactor("get", "images", func(action core.Action) (bool, runtime.Object, error) {
		if getAction, ok := action.(core.GetAction); !ok || getAction.GetName() != dgst.String() {
			t.Errorf("unexpected action: %#+v", action)
			return true, nil, fmt.Errorf("nope, out of luck")
		}
		return true, &imageapiv1.Image{
			ObjectMeta: metav1.ObjectMeta{
				Name: dgst.String(),
			},
			DockerImageReference: dockerImageReference,
		}, nil
	})
	image, err := imageGetter.Get(ctx, dgst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if image.Name != dgst.String() || image.DockerImageReference != dockerImageReference {
		t.Fatalf("unexpected image: %v", image)
	}

	imageClient.PrependReactor("get", "images", func(action core.Action) (bool, runtime.Object, error) {
		t.Errorf("unexpected action: %v", action)
		return true, nil, fmt.Errorf("oops, cache doesn't work")
	})
	image, err = imageGetter.Get(ctx, dgst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if image.Name != dgst.String() || image.DockerImageReference != dockerImageReference {
		t.Fatalf("unexpected image: %v", image)
	}
}
