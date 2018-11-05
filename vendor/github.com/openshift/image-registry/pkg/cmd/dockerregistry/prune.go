package dockerregistry

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/go-units"
	log "github.com/sirupsen/logrus"

	"github.com/docker/distribution/configuration"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/factory"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	registryconfig "github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/prune"
	"github.com/openshift/image-registry/pkg/origin-common/clientcmd"
)

// ExecutePruner runs the pruner.
func ExecutePruner(configFile io.Reader, dryRun bool) {
	config, extraConfig, err := registryconfig.Parse(configFile)
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

	startPrune := "start prune"
	var registryOptions []storage.RegistryOption
	if dryRun {
		startPrune += " (dry-run mode)"
	} else {
		registryOptions = append(registryOptions, storage.EnableDelete)
	}
	dcontext.GetLoggerWithFields(ctx, versionFields()).Info(startPrune)

	registryClient := client.NewRegistryClient(clientcmd.NewConfig().BindToFile(extraConfig.KubeConfig))

	storageDriver, err := factory.Create(config.Storage.Type(), config.Storage.Parameters())
	if err != nil {
		log.Fatalf("error creating storage driver: %s", err)
	}

	registry, err := storage.NewRegistry(ctx, storageDriver, registryOptions...)
	if err != nil {
		log.Fatalf("error creating registry: %s", err)
	}

	var pruner prune.Pruner

	if dryRun {
		pruner = &prune.DryRunPruner{}
	} else {
		pruner = &prune.RegistryPruner{StorageDriver: storageDriver}
	}

	stats, err := prune.Prune(ctx, registry, registryClient, pruner)
	if err != nil {
		log.Error(err)
	}
	if dryRun {
		fmt.Printf("Would delete %d blobs\n", stats.Blobs)
		fmt.Printf("Would free up %s of disk space\n", units.BytesSize(float64(stats.DiskSpace)))
		fmt.Println("Use -prune=delete to actually delete the data")
	} else {
		fmt.Printf("Deleted %d blobs\n", stats.Blobs)
		fmt.Printf("Freed up %s of disk space\n", units.BytesSize(float64(stats.DiskSpace)))
	}
	if err != nil {
		os.Exit(1)
	}
}
