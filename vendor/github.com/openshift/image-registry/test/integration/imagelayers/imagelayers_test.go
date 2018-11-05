package integration

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/docker/distribution"
	"github.com/docker/distribution/context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"

	"github.com/openshift/image-registry/pkg/testframework"
	"github.com/openshift/image-registry/pkg/testutil"
)

// getSchema1Manifest simulates a client which supports only schema 1
// manifests, fetches a manifest from a registry and returns it.
func getSchema1Manifest(repo *testframework.Repository, tag string) (distribution.Manifest, error) {
	c := &http.Client{
		Transport: repo.Transport(),
	}

	resp, err := c.Get(repo.BaseURL() + "/v2/" + repo.RepoName() + "/manifests/" + tag)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get manifest %s:%s: %s", repo.RepoName(), tag, resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s:%s: %v", repo.RepoName(), tag, err)
	}

	m, _, err := distribution.UnmarshalManifest(resp.Header.Get("Content-Type"), body)
	return m, err
}

// TestImageLayers tests that the integrated registry handles schema 1
// manifests and schema 2 manifests consistently and it produces similar Image
// resources for them.
//
// The test relies on the ability of the registry to downconvert manifests.
func TestImageLayers(t *testing.T) {
	master := testframework.NewMaster(t)
	defer master.Close()

	testuser := master.CreateUser("testuser", "testp@ssw0rd")
	testproject := master.CreateProject("image-registry-test-image-layers", testuser.Name)
	imageStreamName := "test-imagelayers"

	registry := master.StartRegistry(t)
	defer registry.Close()

	repo := registry.Repository(testproject.Name, imageStreamName, testuser)

	schema1Tag := "schema1"
	schema2Tag := "schema2"

	ctx := context.Background()

	if _, err := testutil.UploadSchema2Image(ctx, repo, schema2Tag); err != nil {
		t.Fatalf("upload image with schema 2 manifest: %v", err)
	}

	// get the schema2 image's manifest downconverted to a schema 1 manifest
	schema1Manifest, err := getSchema1Manifest(repo, schema2Tag)
	if err != nil {
		t.Fatalf("get schema 1 manifest for image schema2: %v", err)
	}

	if err := testutil.UploadManifest(ctx, repo, schema1Tag, schema1Manifest); err != nil {
		t.Fatalf("upload schema 1 manifest: %v", err)
	}

	imageClient := imageclientv1.NewForConfigOrDie(testuser.KubeConfig())

	schema1ISTag, err := imageClient.ImageStreamTags(testproject.Name).Get(imageStreamName+":"+schema1Tag, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get image stream tag %s:%s: %v", imageStreamName, schema1Tag, err)
	}

	schema2ISTag, err := imageClient.ImageStreamTags(testproject.Name).Get(imageStreamName+":"+schema2Tag, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get image stream tag %s:%s: %v", imageStreamName, schema1Tag, err)
	}

	if schema1ISTag.Image.DockerImageManifestMediaType == schema2ISTag.Image.DockerImageManifestMediaType {
		t.Errorf("expected different media types, but got %q", schema1ISTag.Image.DockerImageManifestMediaType)
	}

	image1LayerOrder := schema1ISTag.Image.Annotations[imageapi.DockerImageLayersOrderAnnotation]
	image2LayerOrder := schema2ISTag.Image.Annotations[imageapi.DockerImageLayersOrderAnnotation]
	if image1LayerOrder != image2LayerOrder {
		t.Errorf("the layer order annotations are different: schema1=%q, schema2=%q", image1LayerOrder, image2LayerOrder)
	} else if image1LayerOrder == "" {
		t.Errorf("the layer order annotation is empty or not present")
	}

	image1Layers := schema1ISTag.Image.DockerImageLayers
	image2Layers := schema2ISTag.Image.DockerImageLayers
	if len(image1Layers) != len(image2Layers) {
		t.Errorf("layers are different: schema1=%#+v, schema2=%#+v", image1Layers, image2Layers)
	} else {
		for i := range image1Layers {
			if image1Layers[i].Name != image2Layers[i].Name {
				t.Errorf("different names for the layer #%d: schema1=%#+v, schema2=%#+v", i, image1Layers[i], image2Layers[i])
			}
			if image1Layers[i].LayerSize != image2Layers[i].LayerSize {
				t.Errorf("different sizes for the layer #%d: schema1=%#+v, schema2=%#+v", i, image1Layers[i], image2Layers[i])
			} else if image1Layers[i].LayerSize <= 0 {
				t.Errorf("unexpected size for the layer #%d: %d", i, image1Layers[i].LayerSize)
			}
		}
	}
}
