package dockerregistry

import (
	"io"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/factory"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	registryconfig "github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/prune"
	"github.com/openshift/image-registry/pkg/origin-common/clientcmd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ExecuteRestore(configFile io.Reader, mode, namespace string) {
	if len(namespace) == 0 {
		namespace = metav1.NamespaceAll
	}

	dockerConfig, config, err := registryconfig.Parse(configFile)
	if err != nil {
		log.Fatalf("error parsing configuration file: %s", err)
	}

	registryClient := client.NewRegistryClient(clientcmd.NewConfig().BindToFile(config.KubeConfig))

	// A lot of installations have the 'debug' log level in their config files,
	// but it's too verbose for pruning. Therefore we ignore it, but we still
	// respect overrides using environment variables.
	dockerConfig.Loglevel = ""
	dockerConfig.Log.Level = configuration.Loglevel(os.Getenv("REGISTRY_LOG_LEVEL"))
	if len(dockerConfig.Log.Level) == 0 {
		dockerConfig.Log.Level = "fatal"
	}

	ctx := context.Background()

	ctx, err = configureLogging(ctx, dockerConfig)
	if err != nil {
		log.Fatalf("error configuring logging: %s", err)
	}

	storageDriver, err := factory.Create(dockerConfig.Storage.Type(), dockerConfig.Storage.Parameters())
	if err != nil {
		log.Fatalf("error creating storage driver: %s", err)
	}

	client, err := registryClient.Client()
	if err != nil {
		log.Fatalf("error getting clients: %v", err)
	}

	registry, err := storage.NewRegistry(ctx, storageDriver)
	if err != nil {
		log.Fatalf("error creating registry: %s", err)
	}

	var restore prune.Restore

	if strings.HasPrefix(mode, "check") {
		restore = &prune.DryRunRestore{}
	} else {
		restore = &prune.StorageRestore{
			Ctx:    ctx,
			Client: client,
		}
	}

	fsck := &prune.Fsck{
		Ctx:        ctx,
		Client:     client,
		Registry:   registry,
		ServerAddr: config.Server.Addr,
		Restore:    restore,
	}

	switch mode {
	case "check", "check-storage", "recover":
		if err := fsck.Storage(namespace); err != nil {
			log.Fatalf("Storage failed: %s", err)
		}
	}

	switch mode {
	case "check", "check-database", "recover":
		if err := fsck.Database(namespace); err != nil {
			log.Fatalf("Database failed: %s", err)
		}
	}
}
