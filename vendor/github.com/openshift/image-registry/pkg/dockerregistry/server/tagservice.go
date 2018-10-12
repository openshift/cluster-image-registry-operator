package server

import (
	"context"

	"github.com/docker/distribution"
	"github.com/docker/distribution/registry/api/v2"

	"github.com/openshift/image-registry/pkg/imagestream"
)

type tagService struct {
	distribution.TagService

	imageStream imagestream.ImageStream
}

func (t tagService) Get(ctx context.Context, tag string) (distribution.Descriptor, error) {
	ok, err := t.imageStream.Exists(ctx)
	if err != nil {
		return distribution.Descriptor{}, err
	}
	if !ok {
		// TODO(dmage): keep the not found error from the master API
		return distribution.Descriptor{}, v2.ErrorCodeNameUnknown.WithDetail(nil)
	}

	tags, err := t.imageStream.Tags(ctx)
	if err != nil {
		return distribution.Descriptor{}, err
	}

	dgst, ok := tags[tag]
	if !ok {
		return distribution.Descriptor{}, distribution.ErrTagUnknown{Tag: tag}
	}

	return distribution.Descriptor{Digest: dgst}, nil
}

func (t tagService) All(ctx context.Context) ([]string, error) {
	ok, err := t.imageStream.Exists(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, distribution.ErrRepositoryUnknown{Name: t.imageStream.Reference()}
	}

	tags, err := t.imageStream.Tags(ctx)
	if err != nil {
		return nil, err
	}

	tagList := []string{}
	for tag := range tags {
		tagList = append(tagList, tag)
	}

	return tagList, nil
}

func (t tagService) Lookup(ctx context.Context, desc distribution.Descriptor) ([]string, error) {
	ok, err := t.imageStream.Exists(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, distribution.ErrRepositoryUnknown{Name: t.imageStream.Reference()}
	}

	tags, err := t.imageStream.Tags(ctx)
	if err != nil {
		return nil, err
	}

	tagList := []string{}
	for tag, dgst := range tags {
		if dgst != desc.Digest {
			continue
		}

		tagList = append(tagList, tag)
	}

	return tagList, nil
}

func (t tagService) Tag(ctx context.Context, tag string, desc distribution.Descriptor) error {
	return nil
}

func (t tagService) Untag(ctx context.Context, tag string) error {
	return nil
}
