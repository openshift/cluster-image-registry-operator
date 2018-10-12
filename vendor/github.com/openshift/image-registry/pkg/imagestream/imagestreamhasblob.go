package imagestream

import (
	"context"
	"sort"
	"time"

	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/opencontainers/go-digest"

	"github.com/openshift/api/image/docker10"
	imageapiv1 "github.com/openshift/api/image/v1"
)

// ByGeneration allows for sorting tag events from latest to oldest.
type ByGeneration []*imageapiv1.TagEvent

func (b ByGeneration) Less(i, j int) bool { return b[i].Generation > b[j].Generation }
func (b ByGeneration) Len() int           { return len(b) }
func (b ByGeneration) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

// HasBlob returns true if the given blob digest is referenced in image stream corresponding to
// given repository. If not found locally, image stream's images will be iterated and fetched from newest to
// oldest until found. Each processed image will update local cache of blobs.
// TODO: remove image lookup path after 3.11
func (is *imageStream) HasBlob(ctx context.Context, dgst digest.Digest) (bool, *imageapiv1.ImageStreamLayers, *imageapiv1.Image) {
	dcontext.GetLogger(ctx).Debugf("verifying presence of blob %q in image stream %s", dgst.String(), is.Reference())
	started := time.Now()
	logFound := func(found bool, layers *imageapiv1.ImageStreamLayers, image *imageapiv1.Image) (bool, *imageapiv1.ImageStreamLayers, *imageapiv1.Image) {
		elapsed := time.Since(started)
		if found {
			dcontext.GetLogger(ctx).Debugf("verified presence of blob %q in image stream %s after %s", dgst.String(), is.Reference(), elapsed.String())
		} else {
			dcontext.GetLogger(ctx).Debugf("detected absence of blob %q in image stream %s after %s", dgst.String(), is.Reference(), elapsed.String())
		}
		return found, layers, image
	}

	// perform the more efficient check for a layer in the image stream
	layers, err := is.imageStreamGetter.layers()
	if err == nil {
		// check for the blob in the layers
		if _, ok := layers.Blobs[dgst.String()]; ok {
			return logFound(true, layers, nil)
		}
		// check for the manifest as a blob
		if _, ok := layers.Images[dgst.String()]; ok {
			return logFound(true, layers, nil)
		}
		return logFound(false, layers, nil)
	}

	// perform the older, O(N) check for a layer in an image stream by scanning over all images

	// TODO: drop this code path after 3.11
	dcontext.GetLogger(ctx).Debugf("API server was unable to fetch layers for the requested image stream: %v", err)

	stream, err := is.imageStreamGetter.get()
	if err != nil {
		dcontext.GetLogger(ctx).Errorf("imageStream.HasBlob: failed to get image stream: %v", err)
		return logFound(false, nil, nil)
	}

	// firstTagEvents holds the first tagevent for each tag
	// so we can quickly scan those first, before checking older
	// tagevents.
	firstTagEvents := []*imageapiv1.TagEvent{}
	olderTagEvents := []*imageapiv1.TagEvent{}
	event2Name := make(map[*imageapiv1.TagEvent]string)
	for _, eventList := range stream.Status.Tags {
		name := eventList.Tag
		for i := range eventList.Items {
			event := &eventList.Items[i]
			if i == 0 {
				firstTagEvents = append(firstTagEvents, event)
			} else {
				olderTagEvents = append(olderTagEvents, event)
			}
			event2Name[event] = name
		}
	}
	// for older tag events, search from youngest to oldest
	sort.Sort(ByGeneration(olderTagEvents))

	tagEvents := append(firstTagEvents, olderTagEvents...)

	processedImages := map[string]struct{}{}

	for _, tagEvent := range tagEvents {
		if _, processed := processedImages[tagEvent.Image]; processed {
			continue
		}

		processedImages[tagEvent.Image] = struct{}{}

		dcontext.GetLogger(ctx).Debugf("getting image %s", tagEvent.Image)
		image, err := is.getImage(ctx, digest.Digest(tagEvent.Image))
		if err != nil {
			if err.Code == ErrImageStreamImageNotFoundCode {
				dcontext.GetLogger(ctx).Debugf("image %q not found", tagEvent.Image)
			} else {
				dcontext.GetLogger(ctx).Errorf("failed to get image: %v", err)
			}
			continue
		}

		if imageHasBlob(ctx, image, dgst) {
			tagName := event2Name[tagEvent]
			dcontext.GetLogger(ctx).Debugf("blob found under istag %s:%s in image %s", is.Reference(), tagName, tagEvent.Image)
			return logFound(true, nil, image)
		}
	}

	dcontext.GetLogger(ctx).Warnf("blob %q exists locally but is not referenced in repository %s", dgst.String(), is.Reference())

	return logFound(false, nil, nil)
}

// imageHasBlob returns true if the image identified by imageName refers to the given blob.
func imageHasBlob(ctx context.Context, image *imageapiv1.Image, blobDigest digest.Digest) bool {
	// someone asks for manifest
	if image.Name == blobDigest.String() {
		return true
	}

	if len(image.DockerImageLayers) == 0 && len(image.DockerImageManifestMediaType) > 0 {
		// If the media type is set, we can safely assume that the best effort to
		// fill the image layers has already been done. There are none.
		return false
	}

	for _, layer := range image.DockerImageLayers {
		if layer.Name == blobDigest.String() {
			return true
		}
	}

	meta, ok := image.DockerImageMetadata.Object.(*docker10.DockerImage)
	if !ok {
		dcontext.GetLogger(ctx).Errorf("image does not have metadata %s", image.Name)
		return false
	}

	// only manifest V2 schema2 has docker image config filled where dockerImage.Metadata.id is its digest
	if image.DockerImageManifestMediaType == schema2.MediaTypeManifest && meta.ID == blobDigest.String() {
		return true
	}

	return false
}
