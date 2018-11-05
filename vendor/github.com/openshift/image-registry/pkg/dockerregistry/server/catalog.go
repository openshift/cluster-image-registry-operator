package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	dcontext "github.com/docker/distribution/context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/cache"

	imageapiv1 "github.com/openshift/api/image/v1"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
)

const paginationEntryTTL = time.Minute

// RepositoryEnumerator allows to enumerate repositories known to the registry.
type RepositoryEnumerator interface {
	// EnumerateRepositories fills the given repos slice with image stream names. The slice's length
	// determines the maximum number of repositories returned. The repositories are lexicographically sorted.
	// The last argument allows for pagination. It is the offset in the catalog. Returned is a number of
	// repositories filled. If there are no more repositories to return, io.EOF is returned.
	EnumerateRepositories(ctx context.Context, repos []string, last string) (n int, err error)
}

// cachingRepositoryEnumerator is an enumerator that supports chunking by caching associations between
// repository names and opaque continuation tokens.
type cachingRepositoryEnumerator struct {
	client client.RegistryClient
	// a cache of opaque continue tokens for repository enumeration
	cache *cache.LRUExpireCache
}

var _ RepositoryEnumerator = &cachingRepositoryEnumerator{}

// NewCachingRepositoryEnumerator returns a new caching repository enumerator.
func NewCachingRepositoryEnumerator(client client.RegistryClient, cache *cache.LRUExpireCache) RepositoryEnumerator {
	return &cachingRepositoryEnumerator{
		client: client,
		cache:  cache,
	}
}

type isHandlerFunc func(is *imageapiv1.ImageStream) error

var errNoSpaceInSlice = errors.New("no space in slice")
var errEnumerationFinished = errors.New("enumeration finished")

func (re *cachingRepositoryEnumerator) EnumerateRepositories(
	ctx context.Context,
	repos []string,
	last string,
) (n int, err error) {
	if len(repos) == 0 {
		// Client explicitly requested 0 results. Returning nil for error seems more appropriate but this is
		// more in line with upstream does. Returning nil actually makes the upstream code panic.
		// TODO: patch upstream?  /_catalog?n=0  results in 500
		return 0, errNoSpaceInSlice
	}

	err = re.enumerateImageStreams(ctx, int64(len(repos)), last, func(is *imageapiv1.ImageStream) error {
		name := fmt.Sprintf("%s/%s", is.Namespace, is.Name)
		repos[n] = name
		n++

		if n >= len(repos) {
			return errEnumerationFinished
		}

		return nil
	})

	switch err {
	case errEnumerationFinished:
		err = nil
	case nil:
		err = io.EOF
	}

	return
}

func (r *cachingRepositoryEnumerator) enumerateImageStreams(
	ctx context.Context,
	limit int64,
	last string,
	handler isHandlerFunc,
) error {
	var (
		start  string
		warned bool
	)

	client, ok := userClientFrom(ctx)
	if !ok {
		dcontext.GetLogger(ctx).Warnf("user token not set, falling back to registry client")
		osClient, err := r.client.Client()
		if err != nil {
			return err
		}
		client = osClient
	}

	if len(last) > 0 {
		if c, ok := r.cache.Get(last); !ok {
			dcontext.GetLogger(ctx).Warnf("failed to find opaque continue token for last repository=%q -> requesting the full image stream list instead of %d items", last, limit)
			warned = true
			limit = 0
		} else {
			start = c.(string)
		}
	}

	iss, err := client.ImageStreams("").List(metav1.ListOptions{
		Limit:    limit,
		Continue: start,
	})
	if apierrors.IsResourceExpired(err) {
		dcontext.GetLogger(ctx).Warnf("continuation token expired (%v) -> requesting the full image stream list", err)
		iss, err = client.ImageStreams("").List(metav1.ListOptions{})
		warned = true
	}

	if err != nil {
		return err
	}

	warnBrokenPagination := func(msg string) {
		if !warned {
			dcontext.GetLogger(ctx).Warnf("pagination not working: %s; the master API does not support chunking", msg)
			warned = true
		}
	}

	if limit > 0 && limit < int64(len(iss.Items)) {
		warnBrokenPagination(fmt.Sprintf("received %d image streams instead of requested maximum %d", len(iss.Items), limit))
	}
	if len(iss.Items) > 0 && len(iss.ListMeta.Continue) > 0 {
		last := iss.Items[len(iss.Items)-1]
		r.cache.Add(fmt.Sprintf("%s/%s", last.Namespace, last.Name), iss.ListMeta.Continue, paginationEntryTTL)
	}

	for _, is := range iss.Items {
		name := fmt.Sprintf("%s/%s", is.Namespace, is.Name)
		if len(last) > 0 && name <= last {
			if !warned {
				warnBrokenPagination(fmt.Sprintf("received unexpected repository name %q -"+
					" lexicographically preceding the requested %q", name, last))
			}
			continue
		}
		err := handler(&is)
		if err != nil {
			return err
		}
	}

	return nil
}
