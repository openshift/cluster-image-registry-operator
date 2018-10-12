package manifesthandler

import (
	"fmt"

	"github.com/opencontainers/go-digest"
)

// ErrManifestBlobBadSize is returned when the blob size in a manifest does
// not match the actual size. The docker/distribution does not check this and
// therefore does not provide an error for this.
type ErrManifestBlobBadSize struct {
	Digest         digest.Digest
	ActualSize     int64
	SizeInManifest int64
}

func (err ErrManifestBlobBadSize) Error() string {
	return fmt.Sprintf("the blob %s has the size (%d) different from the one specified in the manifest (%d)",
		err.Digest, err.ActualSize, err.SizeInManifest)
}
