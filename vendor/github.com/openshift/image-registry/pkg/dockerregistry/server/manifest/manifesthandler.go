package server

import (
	"fmt"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"

	imageapiv1 "github.com/openshift/api/image/v1"
)

// NewFromImage creates a manifest for a manifest stored in the given image.
func NewFromImage(image *imageapiv1.Image) (distribution.Manifest, error) {
	if len(image.DockerImageManifest) == 0 {
		return nil, fmt.Errorf("manifest is not present in image object %s (mediatype=%q)", image.Name, image.DockerImageManifestMediaType)
	}

	switch image.DockerImageManifestMediaType {
	case "", schema1.MediaTypeManifest:
		return unmarshalManifestSchema1([]byte(image.DockerImageManifest), image.DockerImageSignatures)
	case schema2.MediaTypeManifest:
		return unmarshalManifestSchema2([]byte(image.DockerImageManifest))
	default:
		return nil, fmt.Errorf("unsupported manifest media type %s", image.DockerImageManifestMediaType)
	}
}
