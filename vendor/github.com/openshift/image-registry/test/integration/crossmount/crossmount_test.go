package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/distribution"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageapiv1 "github.com/openshift/api/image/v1"
	projectapiv1 "github.com/openshift/api/project/v1"
	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"

	"github.com/openshift/image-registry/pkg/testframework"
	"github.com/openshift/image-registry/pkg/testutil"
)

type errRegistryWantsContent struct {
	src reference.Canonical
	dst reference.Named
}

func (e errRegistryWantsContent) Error() string {
	return fmt.Sprintf("the registry cannot mount %s to %s and wants the content of the blob", e.src, e.dst)
}

func crossMountImage(ctx context.Context, destRepo distribution.Repository, tag string, srcRepoNamed reference.Named, manifest distribution.Manifest) error {
	destBlobs := destRepo.Blobs(ctx)
	for _, desc := range manifest.References() {
		canonicalRef, _ := reference.WithDigest(srcRepoNamed, desc.Digest)
		bw, err := destBlobs.Create(ctx, client.WithMountFrom(canonicalRef))
		if _, ok := err.(distribution.ErrBlobMounted); ok {
			continue
		}
		if err != nil {
			return fmt.Errorf("unable to mount blob %s to %s: %v", canonicalRef, destRepo.Named(), err)
		}
		bw.Cancel(ctx)
		bw.Close()
		return errRegistryWantsContent{
			src: canonicalRef,
			dst: destRepo.Named(),
		}
	}
	if err := testutil.UploadManifest(ctx, destRepo, tag, manifest); err != nil {
		return fmt.Errorf("failed to upload the manifest after cross-mounting blobs: %v", err)
	}
	return nil
}

func copyISTag(t *testing.T, imageClient imageclientv1.ImageV1Interface, destNamespace, destISTag, sourceNamespace, sourceISTag string) error {
	istag := &imageapiv1.ImageStreamTag{
		ObjectMeta: metav1.ObjectMeta{
			Name: destISTag,
		},
		Tag: &imageapiv1.TagReference{
			From: &corev1.ObjectReference{
				Kind:      "ImageStreamTag",
				Name:      sourceISTag,
				Namespace: sourceNamespace,
			},
		},
	}
	_, err := imageClient.ImageStreamTags(destNamespace).Create(istag)
	if err != nil {
		return fmt.Errorf("copy istag %s/%s to %s/%s: %v", sourceNamespace, sourceISTag, destNamespace, destISTag, err)
	}
	return nil
}

func TestCrossMount(t *testing.T) {
	master := testframework.NewMaster(t)
	defer master.Close()

	adminKubeConfig := master.AdminKubeConfig()

	alice := master.CreateUser("alice", "qwerty")
	bob := master.CreateUser("bob", "123456")

	type closeFn func()
	type sourceGenerator func(t *testing.T, registry *testframework.Registry) (*projectapiv1.Project, reference.Named, distribution.Manifest, closeFn)
	type destinationGenerator func(t *testing.T, sourceProject *projectapiv1.Project) (*projectapiv1.Project, string, closeFn)

	noopClose := func() {}

	// As deletion of a project is an async operation, we are going to create each new project with a unique name.
	seq := 0
	uniqueName := func(name string) string {
		seq++
		return fmt.Sprintf("uniq%d-%s", seq, name)
	}

	// Upload a random image to a new project.
	uploadedImage := func(user *testframework.User, repoName string) sourceGenerator {
		return func(t *testing.T, registry *testframework.Registry) (*projectapiv1.Project, reference.Named, distribution.Manifest, closeFn) {
			project := master.CreateProject(uniqueName(user.Name), user.Name)

			repo := registry.Repository(project.Name, repoName, user)
			manifest, err := testutil.UploadSchema2Image(context.Background(), repo, "latest")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("uploaded manifest %s: %v", repo.Named(), manifest)

			named, err := reference.WithName(fmt.Sprintf("%s/%s", project.Name, repoName))
			if err != nil {
				t.Fatal(err)
			}

			return project, named, manifest, noopClose
		}
	}

	// Upload a random image and tag it into a new image stream.
	copiedImage := func(user *testframework.User, repoName string, copiedName string) sourceGenerator {
		return func(t *testing.T, registry *testframework.Registry) (*projectapiv1.Project, reference.Named, distribution.Manifest, closeFn) {
			project, _, manifest, close := uploadedImage(user, repoName)(t, registry)

			imageClient := imageclientv1.NewForConfigOrDie(user.KubeConfig())
			if err := copyISTag(t, imageClient, project.Name, copiedName+":latest", project.Name, repoName+":latest"); err != nil {
				t.Fatal(err)
			}

			named, err := reference.WithName(fmt.Sprintf("%s/%s", project.Name, copiedName))
			if err != nil {
				t.Fatal(err)
			}

			return project, named, manifest, close
		}
	}

	sameProject := func(repoName string) destinationGenerator {
		return func(t *testing.T, sourceProject *projectapiv1.Project) (*projectapiv1.Project, string, closeFn) {
			return sourceProject, repoName, noopClose
		}
	}
	anotherProject := func(user *testframework.User, repoName string) destinationGenerator {
		return func(t *testing.T, sourceProject *projectapiv1.Project) (*projectapiv1.Project, string, closeFn) {
			project := testframework.CreateProject(t, adminKubeConfig, uniqueName(user.Name), user.Name)
			close := func() {
				testframework.DeleteProject(t, adminKubeConfig, project.Name)
			}

			return project, repoName, close
		}
	}

	wantCrossMountError := func(err error) error {
		if _, ok := err.(errRegistryWantsContent); !ok {
			return fmt.Errorf("want a cross-mount error, got %v", err)
		}
		return nil
	}
	wantSuccess := func(err error) error {
		if err != nil {
			return fmt.Errorf("failed to cross-mount image: %v", err)
		}
		return nil
	}

	for _, test := range []struct {
		name        string
		prefix      string
		actor       *testframework.User
		source      sourceGenerator
		destination destinationGenerator
		check       func(error) error
	}{
		{
			name:        "mount own image into the same namespace",
			actor:       alice,
			source:      uploadedImage(alice, "foo"),
			destination: sameProject("mounted-foo"),
			check:       wantSuccess,
		},
		{
			name:        "mount own copied image into the same namespace",
			actor:       alice,
			source:      copiedImage(alice, "foo", "foo-copy"),
			destination: sameProject("mounted-foo-copy"),
			check:       wantSuccess,
		},
		{
			name:        "mount another's image",
			actor:       bob,
			source:      uploadedImage(alice, "foo"),
			destination: anotherProject(bob, "from-foo"),
			check:       wantCrossMountError,
		},
		{
			name:        "mount another's copied image",
			actor:       bob,
			source:      copiedImage(alice, "foo", "foo-copy"),
			destination: anotherProject(bob, "from-foo-copy"),
			check:       wantCrossMountError,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := master.StartRegistry(t)
			defer registry.Close()

			sourceProject, sourceNamed, manifest, sourceCloseFn := test.source(t, registry)
			defer sourceCloseFn()

			destinationProject, destinationImageStream, destinationCloseFn := test.destination(t, sourceProject)
			defer destinationCloseFn()

			t.Log("environment is ready for test")

			destinationRepo := registry.Repository(destinationProject.Name, destinationImageStream, test.actor)
			err := test.check(crossMountImage(context.Background(), destinationRepo, "latest", sourceNamed, manifest))
			if err != nil {
				t.Error(err)
			}
		})
	}
}
