package prune

import (
	"context"
	"fmt"
	"sync"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/storage/driver"
	"github.com/opencontainers/go-digest"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dockerapiv10 "github.com/openshift/api/image/docker10"
	imageapiv1 "github.com/openshift/api/image/v1"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/manifesthandler"
	regstorage "github.com/openshift/image-registry/pkg/dockerregistry/server/storage"
	"github.com/openshift/image-registry/pkg/imagestream"
	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"
	originutil "github.com/openshift/image-registry/pkg/origin-common/util"
)

// Restore defines a common set of operations for database and storage validation
type Restore interface {
	BrokenImage(image imageapiv1.Image, err error) error
	BrokenImageStreamTag(is imageapiv1.ImageStream, tag imageapiv1.NamedTagEventList, pos int, err error) error
	ImageStreamTag(imageStream imagestream.ImageStream, image *imageapiv1.Image, tagName string) error
}

// DryRunRestore prints information about each object
type DryRunRestore struct{}

var _ Restore = &DryRunRestore{}

func (r *DryRunRestore) BrokenImage(image imageapiv1.Image, err error) error {
	fmt.Printf("Broken image %q: %s\n", image.Name, err)
	return nil
}

// BrokenImageStreamTag prints information about broken imagestream tag
func (r *DryRunRestore) BrokenImageStreamTag(is imageapiv1.ImageStream, event imageapiv1.NamedTagEventList, pos int, err error) error {
	ref := is.Namespace + "/" + is.Name + ":" + event.Tag
	dgst := digest.Digest(event.Items[pos].Image)

	if imgErr, ok := err.(*ErrImage); ok {
		if imgErr.Digest == dgst {
			fmt.Printf("Broken imagestream tag %q in position %d: image %q: %s\n", ref, pos, imgErr.Digest, err)
		} else {
			fmt.Printf("Broken imagestream tag %q in position %d: image blob %q: %s\n", ref, pos, imgErr.Digest, err)
		}
	} else {
		fmt.Printf("Broken imagestream tag %q in position %d: %s\n", ref, pos, err)
	}
	return nil
}

// ImageStreamTag prints information about imagestream tag which could be restored
func (r *DryRunRestore) ImageStreamTag(imageStream imagestream.ImageStream, image *imageapiv1.Image, tagName string) error {
	fmt.Printf("Would add image %q to imagestream %q with tag %q\n", image.Name, imageStream.Reference(), tagName)
	return nil
}

type StorageRestore struct {
	DryRunRestore

	Ctx    context.Context
	Client client.Interface
}

var _ Restore = &StorageRestore{}

func (r *StorageRestore) ImageStreamTag(imageStream imagestream.ImageStream, image *imageapiv1.Image, tagName string) error {
	return imageStream.CreateImageStreamMapping(r.Ctx, r.Client, tagName, image)
}

type statter struct {
	mu      sync.Mutex
	statter distribution.BlobStatter
	cache   map[digest.Digest]bool
}

func (s *statter) exists(ctx context.Context, dgst digest.Digest) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache == nil {
		s.cache = make(map[digest.Digest]bool)
	}

	if v, ok := s.cache[dgst]; ok {
		return v, nil
	}

	_, err := s.statter.Stat(ctx, dgst)

	if err != nil && err != distribution.ErrBlobUnknown {
		return false, err
	}

	s.cache[dgst] = err == nil

	return s.cache[dgst], nil
}

// ErrImage defines the error associated with the digest
type ErrImage struct {
	Digest  digest.Digest
	Problem error
}

// Error implements the error interface
func (err ErrImage) Error() string {
	return err.Problem.Error()
}

// Fsck validates or recovers database based on storage
type Fsck struct {
	Ctx        context.Context
	Client     client.Interface
	Registry   distribution.Namespace
	ServerAddr string
	Restore    Restore
}

func (r *Fsck) checkImage(image *imageapiv1.Image, blobStatter *statter) error {
	if !imagestream.IsImageManaged(image) {
		return nil
	}

	if err := originutil.ImageWithMetadata(image); err != nil {
		return fmt.Errorf("error getting image metadata: %s", err)
	}

	imageDigest, err := digest.Parse(image.Name)
	if err != nil {
		return fmt.Errorf("bad image name %q: %s", image.Name, err)
	}

	exists, err := blobStatter.exists(r.Ctx, imageDigest)
	if err != nil {
		return fmt.Errorf("blobStatter failed: %s", err)
	} else if !exists {
		return &ErrImage{
			Digest:  imageDigest,
			Problem: distribution.ErrBlobUnknown,
		}
	}

	if image.DockerImageManifestMediaType == schema2.MediaTypeManifest {
		meta, ok := image.DockerImageMetadata.Object.(*dockerapiv10.DockerImage)
		if ok {
			configDigest, err := digest.Parse(meta.ID)
			if err != nil {
				return fmt.Errorf("image %q: bad config %q: %s", imageDigest, meta.ID, err)
			}

			exists, err := blobStatter.exists(r.Ctx, configDigest)
			if err != nil {
				return fmt.Errorf("image %q: config %q: %s", imageDigest, configDigest, err)
			} else if !exists {
				return &ErrImage{
					Digest:  configDigest,
					Problem: distribution.ErrBlobUnknown,
				}
			}
		}
	}

	for _, layer := range image.DockerImageLayers {
		layerDigest, err := digest.Parse(layer.Name)
		if err != nil {
			return fmt.Errorf("image %q: bad layer %q: %s", imageDigest, layer.Name, err)
		}

		exists, err := blobStatter.exists(r.Ctx, layerDigest)
		if err != nil {
			return fmt.Errorf("image %q: layer %q: %s", imageDigest, layer.Name, err)
		} else if !exists {
			return &ErrImage{
				Digest:  layerDigest,
				Problem: distribution.ErrBlobUnknown,
			}
		}
	}

	return nil
}

// Database checks metadata in the database
func (r *Fsck) Database(namespace string) error {
	listIS, err := r.Client.ImageStreams(namespace).List(metav1.ListOptions{})
	if err != nil {
		if namespace == metav1.NamespaceAll {
			namespace = "all"
		}
		return fmt.Errorf("failed to list image streams in namespace(s): %s, error: %s", namespace, err)
	}

	var image *imageapiv1.Image
	checkedImages := make(map[string]struct{})

	for _, is := range listIS.Items {
		stat := &statter{
			statter: r.Registry.BlobStatter(),
		}

		for _, tagEventList := range is.Status.Tags {
			for i, tagEvent := range tagEventList.Items {
				if _, ok := checkedImages[tagEvent.Image]; ok {
					continue
				}
				checkedImages[tagEvent.Image] = struct{}{}

				imageDigest, err := digest.Parse(tagEvent.Image)
				if err == nil {
					image, err = r.Client.Images().Get(imageDigest.String(), metav1.GetOptions{})
					switch {
					case kerrors.IsNotFound(err):
						image := imageapiv1.Image{}
						image.Name = imageDigest.String()
						if handlerErr := r.Restore.BrokenImage(image, err); handlerErr != nil {
							return fmt.Errorf("BrokenImage failed: %s", handlerErr)
						}
						continue
					case err != nil:
						return &ErrImage{
							Digest:  imageDigest,
							Problem: err,
						}
					}
					err = r.checkImage(image, stat)
					if err == nil {
						continue
					}
				} else {
					err = fmt.Errorf("bad image name %q: %s", tagEvent.Image, err)
				}

				if handlerErr := r.Restore.BrokenImageStreamTag(is, tagEventList, i, err); handlerErr != nil {
					return fmt.Errorf("BrokenImageStreamTag failed: %s", handlerErr)
				}
			}
		}
	}

	listImages, err := r.Client.Images().List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list all images: %s", err)
	}

	for _, image := range listImages.Items {
		if _, ok := checkedImages[image.Name]; ok {
			continue
		}

		stat := &statter{
			statter: r.Registry.BlobStatter(),
		}

		err = r.checkImage(&image, stat)
		if err == nil {
			continue
		}

		err = r.Restore.BrokenImage(image, err)
		if err != nil {
			return fmt.Errorf("BrokenImage failed: %s", err)
		}
	}

	return nil
}

// Storage restores metadata based on the storage
func (r *Fsck) Storage(namespace string) error {
	logger := dcontext.GetLogger(r.Ctx)
	enumStorage := regstorage.Enumerator{Registry: r.Registry}

	err := enumStorage.Repositories(r.Ctx, func(repoName string) error {
		named, err := reference.WithName(repoName)
		if err != nil {
			logger.Errorf("failed to parse the repo name %s: %s", repoName, err)
			return nil
		}

		ref, err := imageapi.ParseDockerImageReference(repoName)
		if err != nil {
			logger.Errorf("failed to parse the image reference %s: %s", repoName, err)
			return nil
		}

		if namespace != metav1.NamespaceAll && namespace != ref.Namespace {
			return nil
		}

		repository, err := r.Registry.Repository(r.Ctx, named)
		if err != nil {
			return fmt.Errorf("failed to open repository: %s: %s", repoName, err)
		}

		manifestService, err := repository.Manifests(r.Ctx)
		if err != nil {
			return fmt.Errorf("failed to create manifest service: %s: %s", repoName, err)
		}

		blobStore := repository.Blobs(r.Ctx)

		imageStream := imagestream.New(r.Ctx, ref.Namespace, ref.Name, r.Client)

		err = enumStorage.Manifests(r.Ctx, repoName, func(dgst digest.Digest) error {
			if _, err := imageStream.ResolveImageID(r.Ctx, dgst); err == nil {
				return nil
			}

			manifest, err := manifestService.Get(r.Ctx, dgst)
			if err != nil {
				logger.Errorf("unable to fetch a manifest %s in the %s repository: %s", dgst, repoName, err)
				return nil
			}

			mh, err := manifesthandler.NewManifestHandler(r.ServerAddr, blobStore, manifest)
			if err != nil {
				logger.Errorf("bad manifest %s in the %s repository: %s", dgst, repoName, err)
				return nil
			}

			if err := mh.Verify(r.Ctx, false); err != nil {
				logger.Errorf("invalid manifest %s in the %s repository: %s", dgst, repoName, err)
				return nil
			}

			config, err := mh.Config(r.Ctx)
			if err != nil {
				logger.Errorf("unable to get a config for manifest %s in the %s repository: %s", dgst, repoName, err)
				return nil
			}

			mediaType, payload, _, err := mh.Payload()
			if err != nil {
				logger.Errorf("unable to get a payload of manifest %s in the %s repository: %s", dgst, repoName, err)
				return nil
			}

			layerOrder, layers, err := mh.Layers(r.Ctx)
			if err != nil {
				logger.Errorf("unable to get a layers of manifest %s in the %s repository: %s", dgst, repoName, err)
				return nil
			}

			image := &imageapiv1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name: dgst.String(),
					Annotations: map[string]string{
						imageapi.ManagedByOpenShiftAnnotation:      "true",
						imageapi.ImageManifestBlobStoredAnnotation: "true",
						imageapi.DockerImageLayersOrderAnnotation:  layerOrder,
					},
				},
				DockerImageReference:         fmt.Sprintf("%s/%s@%s", r.ServerAddr, repoName, dgst.String()),
				DockerImageManifest:          string(payload),
				DockerImageManifestMediaType: mediaType,
				DockerImageConfig:            string(config),
				DockerImageLayers:            layers,
			}

			return r.Restore.ImageStreamTag(imageStream, image, "lost-found-"+dgst.Hex())
		})
		if _, ok := err.(driver.PathNotFoundError); ok {
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to restore images in the image stream %s: %s", repoName, err)
		}

		return nil
	})
	if err != nil {
		switch err := err.(type) {
		case driver.PathNotFoundError:
			return nil
		default:
			return fmt.Errorf("unable to list repositories: %s", err)
		}
	}
	return nil
}
