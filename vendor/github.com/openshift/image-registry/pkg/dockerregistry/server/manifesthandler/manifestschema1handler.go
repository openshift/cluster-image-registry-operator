package manifesthandler

import (
	"context"
	"fmt"
	"path"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/reference"
	"github.com/docker/libtrust"
	"github.com/opencontainers/go-digest"

	imageapiv1 "github.com/openshift/api/image/v1"
	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"
)

type manifestSchema1Handler struct {
	serverAddr string
	blobStore  distribution.BlobStore
	manifest   *schema1.SignedManifest
	blobsCache map[digest.Digest]distribution.Descriptor
}

var _ ManifestHandler = &manifestSchema1Handler{}

func (h *manifestSchema1Handler) Config(ctx context.Context) ([]byte, error) {
	return nil, nil
}

func (h *manifestSchema1Handler) Digest() (digest.Digest, error) {
	return digest.FromBytes(h.manifest.Canonical), nil
}

func (h *manifestSchema1Handler) Manifest() distribution.Manifest {
	return h.manifest
}

func (h *manifestSchema1Handler) statBlob(ctx context.Context, dgst digest.Digest) (distribution.Descriptor, error) {
	desc, ok := h.blobsCache[dgst]
	if ok {
		return desc, nil
	}

	desc, err := h.blobStore.Stat(ctx, dgst)
	if err != nil {
		return desc, err
	}

	if h.blobsCache == nil {
		h.blobsCache = make(map[digest.Digest]distribution.Descriptor)
	}
	h.blobsCache[dgst] = desc

	return desc, nil
}

func (h *manifestSchema1Handler) Layers(ctx context.Context) (string, []imageapiv1.ImageLayer, error) {
	layers := make([]imageapiv1.ImageLayer, len(h.manifest.FSLayers))
	for i, fslayer := range h.manifest.FSLayers {
		desc, err := h.statBlob(ctx, fslayer.BlobSum)
		if err != nil {
			return "", nil, err
		}

		// In a schema1 manifest the layers are ordered from the youngest to
		// the oldest. But we want to have layers in different order.
		revidx := (len(h.manifest.FSLayers) - 1) - i // n-1, n-2, ..., 1, 0

		layers[revidx].Name = fslayer.BlobSum.String()
		layers[revidx].LayerSize = desc.Size
		layers[revidx].MediaType = schema1.MediaTypeManifestLayer
	}
	return imageapi.DockerImageLayersOrderAscending, layers, nil
}

func (h *manifestSchema1Handler) Payload() (mediaType string, payload []byte, canonical []byte, err error) {
	mt, payload, err := h.manifest.Payload()
	return mt, payload, h.manifest.Canonical, err
}

func (h *manifestSchema1Handler) Verify(ctx context.Context, skipDependencyVerification bool) error {
	var errs distribution.ErrManifestVerification

	// we want to verify that referenced blobs exist locally or accessible over
	// pullthroughBlobStore. The base image of this image can be remote repository
	// and since we use pullthroughBlobStore all the layer existence checks will be
	// successful. This means that the docker client will not attempt to send them
	// to us as it will assume that the registry has them.

	if len(path.Join(h.serverAddr, h.manifest.Name)) > reference.NameTotalLengthMax {
		errs = append(errs,
			distribution.ErrManifestNameInvalid{
				Name:   h.manifest.Name,
				Reason: fmt.Errorf("<registry-host>/<manifest-name> must not be more than %d characters", reference.NameTotalLengthMax),
			})
	}

	if !reference.NameRegexp.MatchString(h.manifest.Name) {
		errs = append(errs,
			distribution.ErrManifestNameInvalid{
				Name:   h.manifest.Name,
				Reason: fmt.Errorf("invalid manifest name format"),
			})
	}

	if len(h.manifest.History) != len(h.manifest.FSLayers) {
		errs = append(errs, fmt.Errorf("mismatched history and fslayer cardinality %d != %d",
			len(h.manifest.History), len(h.manifest.FSLayers)))
	}

	if _, err := schema1.Verify(h.manifest); err != nil {
		switch err {
		case libtrust.ErrMissingSignatureKey, libtrust.ErrInvalidJSONContent, libtrust.ErrMissingSignatureKey:
			errs = append(errs, distribution.ErrManifestUnverified{})
		default:
			if err.Error() == "invalid signature" {
				errs = append(errs, distribution.ErrManifestUnverified{})
			} else {
				errs = append(errs, err)
			}
		}
	}

	if skipDependencyVerification {
		if len(errs) > 0 {
			return errs
		}
		return nil
	}

	for _, fsLayer := range h.manifest.References() {
		_, err := h.statBlob(ctx, fsLayer.Digest)
		if err != nil {
			if err != distribution.ErrBlobUnknown {
				errs = append(errs, err)
				continue
			}

			// On error here, we always append unknown blob errors.
			errs = append(errs, distribution.ErrManifestBlobUnknown{Digest: fsLayer.Digest})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
