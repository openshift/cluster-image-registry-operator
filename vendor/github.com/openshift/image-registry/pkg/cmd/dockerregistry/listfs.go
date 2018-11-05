package dockerregistry

import (
	"fmt"
	"io"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/factory"
	"github.com/opencontainers/go-digest"

	registryconfig "github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	regstorage "github.com/openshift/image-registry/pkg/dockerregistry/server/storage"
)

type ListOptions struct {
	Repositories      bool
	Blobs             bool
	Manifests         bool
	ManifestsFromRepo string
}

func ExecuteListFS(configFile io.Reader, opts *ListOptions) {
	config, _, err := registryconfig.Parse(configFile)
	if err != nil {
		log.Fatalf("error parsing configuration file: %s", err)
	}

	// A lot of installations have the 'debug' log level in their config files,
	// but it's too verbose for pruning. Therefore we ignore it, but we still
	// respect overrides using environment variables.
	config.Loglevel = ""
	config.Log.Level = configuration.Loglevel(os.Getenv("REGISTRY_LOG_LEVEL"))
	if len(config.Log.Level) == 0 {
		config.Log.Level = "warning"
	}

	ctx := context.Background()
	ctx, err = configureLogging(ctx, config)
	if err != nil {
		log.Fatalf("error configuring logging: %s", err)
	}

	storageDriver, err := factory.Create(config.Storage.Type(), config.Storage.Parameters())
	if err != nil {
		log.Fatalf("error creating storage driver: %s", err)
	}

	registry, err := storage.NewRegistry(ctx, storageDriver)
	if err != nil {
		log.Fatalf("error creating registry: %s", err)
	}

	enumStorage := regstorage.Enumerator{Registry: registry}

	var repoList []string

	if opts.Repositories {
		err := enumStorage.Repositories(ctx, func(repoName string) error {
			fmt.Printf("repository\t%s\n", repoName)
			repoList = append(repoList, repoName)
			return nil
		})
		if err != nil {
			log.Fatalf("unable to list repositories: %s", err)
		}
	}

	if opts.Blobs {
		err := enumStorage.Blobs(ctx, func(dgst digest.Digest) error {
			fmt.Printf("blob\t%s\n", dgst)
			return nil
		})
		if err != nil {
			log.Fatalf("unable to list repositories: %s", err)
		}
	}

	if opts.Manifests {
		if len(opts.ManifestsFromRepo) > 0 {
			repoList = []string{opts.ManifestsFromRepo}
		} else if !opts.Repositories {
			err := enumStorage.Repositories(ctx, func(repoName string) error {
				repoList = append(repoList, repoName)
				return nil
			})
			if err != nil {
				log.Fatalf("unable to list repositories: %s", err)
			}
		}

		for _, repoName := range repoList {
			err := enumStorage.Manifests(ctx, repoName, func(dgst digest.Digest) error {
				fmt.Printf("manifest\t%s\t%s\n", repoName, dgst)
				return nil
			})
			if err != nil {
				log.Fatalf("unable to list manifests in the %q repository: %s", repoName, err)
			}
		}
	}
}
