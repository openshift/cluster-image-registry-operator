package storage

import (
	"fmt"

	"github.com/docker/distribution/configuration"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/driver/factory"

	registryconfig "github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	"github.com/openshift/image-registry/pkg/testframework"
)

const Name = "integration"

type storageDriverFactory struct{}

func (f *storageDriverFactory) Create(parameters map[string]interface{}) (storagedriver.StorageDriver, error) {
	driver, ok := parameters["driver"].(storagedriver.StorageDriver)
	if !ok {
		return nil, fmt.Errorf("unable to get driver from %#+v", parameters["driver"])
	}
	return driver, nil
}

func init() {
	factory.Register(Name, &storageDriverFactory{})
}

type withDriver struct {
	driver storagedriver.StorageDriver
}

func WithDriver(driver storagedriver.StorageDriver) testframework.RegistryOption {
	return &withDriver{driver: driver}
}

func (o *withDriver) Apply(dockerConfig *configuration.Configuration, extraConfig *registryconfig.Configuration) {
	delete(dockerConfig.Storage, dockerConfig.Storage.Type())
	dockerConfig.Storage[Name] = configuration.Parameters{
		"driver": o.driver,
	}
}
