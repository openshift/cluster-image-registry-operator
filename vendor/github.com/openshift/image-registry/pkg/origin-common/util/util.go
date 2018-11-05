package util

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/golang/glog"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	dockerapiv10 "github.com/openshift/api/image/docker10"
	imageapiv1 "github.com/openshift/api/image/v1"
	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"
)

func fillImageLayers(image *imageapi.Image, manifest imageapi.DockerImageManifest) error {
	if len(image.DockerImageLayers) != 0 {
		// DockerImageLayers is already filled by the registry.
		return nil
	}

	switch manifest.SchemaVersion {
	case 1:
		if len(manifest.History) != len(manifest.FSLayers) {
			return fmt.Errorf("the image %s (%s) has mismatched history and fslayer cardinality (%d != %d)", image.Name, image.DockerImageReference, len(manifest.History), len(manifest.FSLayers))
		}

		image.DockerImageLayers = make([]imageapi.ImageLayer, len(manifest.FSLayers))
		for i, obj := range manifest.History {
			layer := manifest.FSLayers[i]

			var size imageapi.DockerV1CompatibilityImageSize
			if err := json.Unmarshal([]byte(obj.DockerV1Compatibility), &size); err != nil {
				size.Size = 0
			}

			// reverse order of the layers: in schema1 manifests the
			// first layer is the youngest (base layers are at the
			// end), but we want to store layers in the Image resource
			// in order from the oldest to the youngest.
			revidx := (len(manifest.History) - 1) - i // n-1, n-2, ..., 1, 0

			image.DockerImageLayers[revidx].Name = layer.DockerBlobSum
			image.DockerImageLayers[revidx].LayerSize = size.Size
			image.DockerImageLayers[revidx].MediaType = schema1.MediaTypeManifestLayer
		}
	case 2:
		// The layer list is ordered starting from the base image (opposite order of schema1).
		// So, we do not need to change the order of layers.
		image.DockerImageLayers = make([]imageapi.ImageLayer, len(manifest.Layers))
		for i, layer := range manifest.Layers {
			image.DockerImageLayers[i].Name = layer.Digest
			image.DockerImageLayers[i].LayerSize = layer.Size
			image.DockerImageLayers[i].MediaType = layer.MediaType
		}
	default:
		return fmt.Errorf("unrecognized Docker image manifest schema %d for %q (%s)", manifest.SchemaVersion, image.Name, image.DockerImageReference)
	}

	if image.Annotations == nil {
		image.Annotations = map[string]string{}
	}
	image.Annotations[imageapi.DockerImageLayersOrderAnnotation] = imageapi.DockerImageLayersOrderAscending

	return nil
}

// InternalImageWithMetadata mutates the given image. It parses raw DockerImageManifest data stored in the image and
// fills its DockerImageMetadata and other fields.
func InternalImageWithMetadata(image *imageapi.Image) error {
	if len(image.DockerImageManifest) == 0 {
		return nil
	}

	ReorderImageLayers(image)

	if len(image.DockerImageLayers) > 0 && image.DockerImageMetadata.Size > 0 && len(image.DockerImageManifestMediaType) > 0 {
		glog.V(5).Infof("Image metadata already filled for %s", image.Name)
		return nil
	}

	manifest := imageapi.DockerImageManifest{}
	if err := json.Unmarshal([]byte(image.DockerImageManifest), &manifest); err != nil {
		return err
	}

	err := fillImageLayers(image, manifest)
	if err != nil {
		return err
	}

	switch manifest.SchemaVersion {
	case 1:
		image.DockerImageManifestMediaType = schema1.MediaTypeManifest

		if len(manifest.History) == 0 {
			// It should never have an empty history, but just in case.
			return fmt.Errorf("the image %s (%s) has a schema 1 manifest, but it doesn't have history", image.Name, image.DockerImageReference)
		}

		v1Metadata := imageapi.DockerV1CompatibilityImage{}
		if err := json.Unmarshal([]byte(manifest.History[0].DockerV1Compatibility), &v1Metadata); err != nil {
			return err
		}

		image.DockerImageMetadata.ID = v1Metadata.ID
		image.DockerImageMetadata.Parent = v1Metadata.Parent
		image.DockerImageMetadata.Comment = v1Metadata.Comment
		image.DockerImageMetadata.Created = v1Metadata.Created
		image.DockerImageMetadata.Container = v1Metadata.Container
		image.DockerImageMetadata.ContainerConfig = v1Metadata.ContainerConfig
		image.DockerImageMetadata.DockerVersion = v1Metadata.DockerVersion
		image.DockerImageMetadata.Author = v1Metadata.Author
		image.DockerImageMetadata.Config = v1Metadata.Config
		image.DockerImageMetadata.Architecture = v1Metadata.Architecture
	case 2:
		image.DockerImageManifestMediaType = schema2.MediaTypeManifest

		if len(image.DockerImageConfig) == 0 {
			return fmt.Errorf("dockerImageConfig must not be empty for manifest schema 2")
		}

		config := imageapi.DockerImageConfig{}
		if err := json.Unmarshal([]byte(image.DockerImageConfig), &config); err != nil {
			return fmt.Errorf("failed to parse dockerImageConfig: %v", err)
		}

		image.DockerImageMetadata.ID = manifest.Config.Digest
		image.DockerImageMetadata.Parent = config.Parent
		image.DockerImageMetadata.Comment = config.Comment
		image.DockerImageMetadata.Created = config.Created
		image.DockerImageMetadata.Container = config.Container
		image.DockerImageMetadata.ContainerConfig = config.ContainerConfig
		image.DockerImageMetadata.DockerVersion = config.DockerVersion
		image.DockerImageMetadata.Author = config.Author
		image.DockerImageMetadata.Config = config.Config
		image.DockerImageMetadata.Architecture = config.Architecture
	default:
		return fmt.Errorf("unrecognized Docker image manifest schema %d for %q (%s)", manifest.SchemaVersion, image.Name, image.DockerImageReference)
	}

	layerSet := sets.NewString()
	if manifest.SchemaVersion == 2 {
		layerSet.Insert(manifest.Config.Digest)
		image.DockerImageMetadata.Size = int64(len(image.DockerImageConfig))
	} else {
		image.DockerImageMetadata.Size = 0
	}
	for _, layer := range image.DockerImageLayers {
		if layerSet.Has(layer.Name) {
			continue
		}
		layerSet.Insert(layer.Name)
		image.DockerImageMetadata.Size += layer.LayerSize
	}

	return nil
}

// ReorderImageLayers mutates the given image. It reorders the layers in ascending order.
// Ascending order matches the order of layers in schema 2. Schema 1 has reversed (descending) order of layers.
func ReorderImageLayers(image *imageapi.Image) {
	if len(image.DockerImageLayers) == 0 {
		return
	}

	layersOrder, ok := image.Annotations[imageapi.DockerImageLayersOrderAnnotation]
	if !ok {
		switch image.DockerImageManifestMediaType {
		case schema1.MediaTypeManifest, schema1.MediaTypeSignedManifest:
			layersOrder = imageapi.DockerImageLayersOrderAscending
		case schema2.MediaTypeManifest:
			layersOrder = imageapi.DockerImageLayersOrderDescending
		default:
			return
		}
	}

	if layersOrder == imageapi.DockerImageLayersOrderDescending {
		// reverse order of the layers (lowest = 0, highest = i)
		for i, j := 0, len(image.DockerImageLayers)-1; i < j; i, j = i+1, j-1 {
			image.DockerImageLayers[i], image.DockerImageLayers[j] = image.DockerImageLayers[j], image.DockerImageLayers[i]
		}
	}

	if image.Annotations == nil {
		image.Annotations = map[string]string{}
	}

	image.Annotations[imageapi.DockerImageLayersOrderAnnotation] = imageapi.DockerImageLayersOrderAscending
}

// ImageWithMetadata mutates the given image. It parses raw DockerImageManifest data stored in the image and
// fills its DockerImageMetadata and other fields.
// Copied from v3.7 github.com/openshift/origin/pkg/image/apis/image/v1/helpers.go
func ImageWithMetadata(image *imageapiv1.Image) error {
	// Check if the metadata are already filled in for this image.
	meta, hasMetadata := image.DockerImageMetadata.Object.(*dockerapiv10.DockerImage)
	if hasMetadata && meta.Size > 0 {
		return nil
	}

	version := image.DockerImageMetadataVersion
	if len(version) == 0 {
		version = "1.0"
	}

	obj := &dockerapiv10.DockerImage{}
	if len(image.DockerImageMetadata.Raw) != 0 {
		if err := json.Unmarshal(image.DockerImageMetadata.Raw, obj); err != nil {
			return err
		}
		image.DockerImageMetadata.Object = obj
	}

	image.DockerImageMetadataVersion = version

	return nil
}

// LatestImageTagEvent returns the most recent TagEvent and the tag for the specified
// image.
// Copied from v3.7 github.com/openshift/origin/pkg/image/apis/image/v1/helpers.go
func LatestImageTagEvent(stream *imageapiv1.ImageStream, imageID string) (string, *imageapiv1.TagEvent) {
	var (
		latestTagEvent *imageapiv1.TagEvent
		latestTag      string
	)
	for _, events := range stream.Status.Tags {
		if len(events.Items) == 0 {
			continue
		}
		tag := events.Tag
		for i, event := range events.Items {
			if imageapi.DigestOrImageMatch(event.Image, imageID) &&
				(latestTagEvent == nil || latestTagEvent != nil && event.Created.After(latestTagEvent.Created.Time)) {
				latestTagEvent = &events.Items[i]
				latestTag = tag
			}
		}
	}
	return latestTag, latestTagEvent
}

// ResolveImageID returns latest TagEvent for specified imageID and an error if
// there's more than one image matching the ID or when one does not exist.
// Copied from v3.7 github.com/openshift/origin/pkg/image/apis/image/v1/helpers.go
func ResolveImageID(stream *imageapiv1.ImageStream, imageID string) (*imageapiv1.TagEvent, error) {
	var event *imageapiv1.TagEvent
	set := sets.NewString()
	for _, history := range stream.Status.Tags {
		for i := range history.Items {
			tagging := &history.Items[i]
			if imageapi.DigestOrImageMatch(tagging.Image, imageID) {
				event = tagging
				set.Insert(tagging.Image)
			}
		}
	}
	switch len(set) {
	case 1:
		return &imageapiv1.TagEvent{
			Created:              metav1.Now(),
			DockerImageReference: event.DockerImageReference,
			Image:                event.Image,
		}, nil
	case 0:
		return nil, kerrors.NewNotFound(imageapiv1.Resource("imagestreamimage"), imageID)
	default:
		return nil, kerrors.NewConflict(imageapiv1.Resource("imagestreamimage"), imageID, fmt.Errorf("multiple images match the prefix %q: %s", imageID, strings.Join(set.List(), ", ")))
	}
}

// LatestTaggedImage returns the most recent TagEvent for the specified image
// repository and tag. Will resolve lookups for the empty tag. Returns nil
// if tag isn't present in stream.status.tags.
func LatestTaggedImage(stream *imageapiv1.ImageStream, tag string) *imageapiv1.TagEvent {
	if len(tag) == 0 {
		tag = imageapi.DefaultImageTag
	}
	for _, tagEventList := range stream.Status.Tags {
		if tagEventList.Tag != tag {
			continue
		}
		if len(tagEventList.Items) == 0 {
			return nil
		}
		return &tagEventList.Items[0]
	}

	return nil
}
