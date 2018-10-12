package server

import (
	"context"
	"io"
	"testing"
	"time"

	registryclient "github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	"github.com/openshift/image-registry/pkg/testutil"
)

func TestCatalog(t *testing.T) {
	const blobRepoCacheTTL = time.Millisecond * 500

	type isMeta struct{ namespace, name string }

	for _, tc := range []struct {
		name          string
		isObjectMeta  []isMeta
		buffer        []string
		last          string
		expectedRepos []string
		expectedError error
	}{
		{
			name:          "no image streams",
			buffer:        make([]string, 2),
			expectedError: io.EOF,
		},

		{
			name:          "one image stream",
			isObjectMeta:  []isMeta{{"nm", "foo"}},
			buffer:        make([]string, 2),
			expectedRepos: []string{"nm/foo"},
			expectedError: io.EOF,
		},

		{
			name:          "2 image streams in the same namespace",
			isObjectMeta:  []isMeta{{"nm", "foo"}, {"nm", "bar"}},
			buffer:        make([]string, 2),
			expectedRepos: []string{"nm/bar", "nm/foo"},
			expectedError: nil,
		},

		{
			name:          "3 image streams in different namespaces",
			isObjectMeta:  []isMeta{{"fst", "is"}, {"snd", "is"}, {"trd", "name"}},
			buffer:        make([]string, 4),
			expectedRepos: []string{"fst/is", "snd/is", "trd/name"},
			expectedError: io.EOF,
		},

		{
			name: "repositories get sorted",
			isObjectMeta: []isMeta{
				{"nm", "ab"}, {"nmc", "aa"}, {"ab", "cd"}, {"ss", "is"}, {"ab", "aa"}, {"a", "nn"},
			},
			buffer:        make([]string, 7),
			expectedRepos: []string{"a/nn", "ab/aa", "ab/cd", "nm/ab", "nmc/aa", "ss/is"},
			expectedError: io.EOF,
		},

		{
			name:          "short buffer",
			isObjectMeta:  []isMeta{{"nm", "foo"}, {"nm", "bar"}},
			buffer:        make([]string, 1),
			expectedRepos: []string{"nm/bar"},
			expectedError: nil,
		},

		{
			name:          "skip the first",
			isObjectMeta:  []isMeta{{"nm", "foo"}, {"nm", "bar"}},
			buffer:        make([]string, 2),
			last:          "nm/bar",
			expectedRepos: []string{"nm/foo"},
			expectedError: io.EOF,
		},

		{
			name:          "skip the last",
			isObjectMeta:  []isMeta{{"nm", "foo"}, {"nm", "bar"}},
			buffer:        make([]string, 2),
			last:          "nm/foo",
			expectedRepos: []string{},
			expectedError: io.EOF,
		},

		{
			name:          "bigger buffer capacity does not matter",
			isObjectMeta:  []isMeta{{"nm", "foo"}, {"nm", "bar"}},
			buffer:        make([]string, 1, 2),
			expectedRepos: []string{"nm/bar"},
			expectedError: nil,
		},

		{
			name:          "pick a repository in the middle",
			isObjectMeta:  []isMeta{{"bar", "is"}, {"baz", "is"}, {"foo", "is"}},
			buffer:        make([]string, 1),
			last:          "bar/is",
			expectedRepos: []string{"baz/is"},
			expectedError: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = testutil.WithTestLogger(ctx, t)
			ctx = withAuthPerformed(ctx)

			fos, imageClient := testutil.NewFakeOpenShiftWithClient(ctx)
			for _, is := range tc.isObjectMeta {
				testutil.AddImageStream(t, fos, is.namespace, is.name, nil)
			}

			reg, err := newTestRegistry(
				ctx,
				registryclient.NewFakeRegistryAPIClient(nil, imageClient),
				nil,
				blobRepoCacheTTL,
				false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			numFilled, err := reg.Repositories(ctx, tc.buffer, tc.last)
			if a, e := err, tc.expectedError; a != e {
				t.Errorf("got unexpected error: %q != %q", a, e)
			}

			if a, e := numFilled, len(tc.expectedRepos); a != e {
				t.Errorf("got unexpected number of repos: %d != %d", numFilled, e)
			}

			for i := 0; i < numFilled || i < len(tc.expectedRepos); i++ {
				if i < numFilled && i >= len(tc.expectedRepos) {
					t.Errorf("got unexpected repository at position #%d: %q", i, tc.buffer[i])
				} else if i < len(tc.expectedRepos) && i >= numFilled {
					t.Errorf("expected repository %q not returned", tc.expectedRepos[i])
				} else if a, e := tc.buffer[i], tc.expectedRepos[i]; a != e {
					t.Errorf("got unexpected repository at position #%d: %q != %q", i, a, e)
				}
			}
		})
	}
}
