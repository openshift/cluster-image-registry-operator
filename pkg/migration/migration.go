package migration

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/docker/distribution/configuration"
	registryconfig "github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"

	coreapi "k8s.io/api/core/v1"

	appsapi "github.com/openshift/api/apps/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/migration/dependency"
)

func getVolumeSource(volumes []coreapi.Volume, name string) (coreapi.VolumeSource, bool) {
	for _, volume := range volumes {
		if volume.Name == name {
			return volume.VolumeSource, true
		}
	}
	return coreapi.VolumeSource{}, false
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

func migrateParameters(params map[string]interface{}, rules map[string]interface{}) error {
	for key, rule := range rules {
		ruleVal := reflect.ValueOf(rule)
		if ruleVal.Type().Kind() != reflect.Func ||
			ruleVal.Type().NumIn() != 2 ||
			ruleVal.Type().NumOut() != 1 ||
			ruleVal.Type().In(1).Kind() != reflect.Bool ||
			!ruleVal.Type().Out(0).Implements(errorType) {
			panic(fmt.Errorf("migrateParameters: rule for %s is invalid, should be `func(value TYPE, ok bool) error`", key))
		}

		inType := ruleVal.Type().In(0)
		value, ok := params[key]
		if !ok {
			value = reflect.Zero(inType).Interface()
		} else if !reflect.ValueOf(value).Type().AssignableTo(inType) {
			return fmt.Errorf("failed to migrate the field %q: got %T, expected %s", key, value, inType)
		}
		out := ruleVal.Call([]reflect.Value{
			reflect.ValueOf(value),
			reflect.ValueOf(ok),
		})
		err := out[0].Interface()
		if err != nil {
			return fmt.Errorf("failed to migrate the field %q: %s", key, err)
		}
	}
	for key := range params {
		if _, ok := rules[key]; !ok {
			return fmt.Errorf("no rules to migrate the field %q", key)
		}
	}
	return nil
}

func newImageRegistryConfigStorage(config *configuration.Configuration, dc *appsapi.DeploymentConfig, registry coreapi.Container) (imageregistryapi.ImageRegistryConfigStorage, error) {
	var emptyConfig imageregistryapi.ImageRegistryConfigStorage

	storageType := config.Storage.Type()
	params := config.Storage.Parameters()
	switch storageType {
	case "filesystem":
		err := migrateParameters(params, map[string]interface{}{
			"rootdirectory": func(rootdirectory string, ok bool) error {
				if !ok {
					return fmt.Errorf("the field is required")
				}
				if rootdirectory != "/registry" {
					return fmt.Errorf("rootdirectory must be /registry")
				}
				return nil
			},
		})
		if err != nil {
			return emptyConfig, fmt.Errorf("failed to migrate parameters for the storage %s: %s", storageType, err)
		}

		volumeSource := coreapi.VolumeSource{
			EmptyDir: &coreapi.EmptyDirVolumeSource{},
		}
		for _, volumeMount := range registry.VolumeMounts {
			if volumeMount.MountPath == "/registry" {
				if volumeMount.SubPath != "" {
					return emptyConfig, fmt.Errorf("the volume mount for /registry has subpath")
				}
				var ok bool
				volumeSource, ok = getVolumeSource(dc.Spec.Template.Spec.Volumes, volumeMount.Name)
				if !ok {
					return emptyConfig, fmt.Errorf("unable to find the volume %q for /registry in the pod spec", volumeMount.Name)
				}
				break
			}
		}

		return imageregistryapi.ImageRegistryConfigStorage{
			Filesystem: &imageregistryapi.ImageRegistryConfigStorageFilesystem{
				VolumeSource: volumeSource,
			},
		}, nil
	case "s3":
		storageS3 := &imageregistryapi.ImageRegistryConfigStorageS3{}
		err := migrateParameters(params, map[string]interface{}{
			"bucket": func(bucket string, ok bool) error {
				storageS3.Bucket = bucket
				return nil
			},
			"region": func(region string, ok bool) error {
				storageS3.Region = region
				return nil
			},
			"regionendpoint": func(regionEndpoint string, ok bool) error {
				storageS3.RegionEndpoint = regionEndpoint
				return nil
			},
			"encrypt": func(encrypt bool, ok bool) error {
				storageS3.Encrypt = encrypt
				return nil
			},
		})
		if err != nil {
			return emptyConfig, fmt.Errorf("failed to migrate parameters for the storage %s: %s", storageType, err)
		}
		return imageregistryapi.ImageRegistryConfigStorage{
			S3: storageS3,
		}, nil
	case "azure":
		storageAzure := &imageregistryapi.ImageRegistryConfigStorageAzure{}
		err := migrateParameters(params, map[string]interface{}{
			"container": func(container string, ok bool) error {
				storageAzure.Container = container
				return nil
			},
		})
		if err != nil {
			return emptyConfig, fmt.Errorf("failed to migrate parameters for the storage %s: %s", storageType, err)
		}
		return imageregistryapi.ImageRegistryConfigStorage{
			Azure: storageAzure,
		}, nil
	case "gcs":
		storageGCS := &imageregistryapi.ImageRegistryConfigStorageGCS{}
		err := migrateParameters(params, map[string]interface{}{
			"bucket": func(bucket string, ok bool) error {
				storageGCS.Bucket = bucket
				return nil
			},
		})
		if err != nil {
			return emptyConfig, fmt.Errorf("failed to migrate parameters for the storage %s: %s", storageType, err)
		}
		return imageregistryapi.ImageRegistryConfigStorage{
			GCS: storageGCS,
		}, nil
	case "swift":
		storageSwift := &imageregistryapi.ImageRegistryConfigStorageSwift{}
		err := migrateParameters(params, map[string]interface{}{
			"authurl": func(authURL string, ok bool) error {
				storageSwift.AuthURL = authURL
				return nil
			},
			"container": func(container string, ok bool) error {
				storageSwift.Container = container
				return nil
			},
		})
		if err != nil {
			return emptyConfig, fmt.Errorf("failed to migrate parameters for the storage %s: %s", storageType, err)
		}
		return imageregistryapi.ImageRegistryConfigStorage{
			Swift: storageSwift,
		}, nil
	default:
		return emptyConfig, fmt.Errorf("unsupported storage type %s", storageType)
	}
}

func migrateTLS(config *configuration.Configuration, podFileGetter PodFileGetter) (bool, *coreapi.Secret, error) {
	if config.HTTP.TLS.Key == "" && config.HTTP.TLS.Certificate == "" {
		return false, nil, nil
	}
	if len(config.HTTP.TLS.ClientCAs) > 0 {
		return false, nil, fmt.Errorf("HTTP TLS ClientCAs is not supported")
	}
	if config.HTTP.TLS.LetsEncrypt.Email != "" {
		return false, nil, fmt.Errorf("HTTP TLS LetsEncrypt is not supported")
	}

	key, err := podFileGetter.PodFile(config.HTTP.TLS.Key)
	if err != nil {
		return false, nil, fmt.Errorf("get TLS key: %s", err)
	}

	certificate, err := podFileGetter.PodFile(config.HTTP.TLS.Certificate)
	if err != nil {
		return false, nil, fmt.Errorf("get TLS certificate: %s", err)
	}

	return true, &coreapi.Secret{
		Data: map[string][]byte{
			"tls.key": key,
			"tls.crt": certificate,
		},
	}, nil
}

func isEnvVarSupported(name string) bool {
	return strings.HasPrefix(name, "REGISTRY_") ||
		name == "DOCKER_REGISTRY_URL" ||
		name == "OPENSHIFT_DEFAULT_REGISTRY"
}

func NewImageRegistrySpecFromDeploymentConfig(dc *appsapi.DeploymentConfig, resources dependency.NamespacedResources) (imageregistryapi.ImageRegistrySpec, *coreapi.Secret, error) {
	fail := func(format string, a ...interface{}) (imageregistryapi.ImageRegistrySpec, *coreapi.Secret, error) {
		format = "unable to create an image registry spec from the DeploymentConfig %s/%s: " + format
		a = append([]interface{}{dc.Namespace, dc.Name}, a...)
		return imageregistryapi.ImageRegistrySpec{}, nil, fmt.Errorf(format, a...)
	}

	containers := dc.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		return fail("got %d containers, want 1", len("containers"))
	}

	registry := containers[0]
	if registry.Name != "registry" {
		return fail("got container named %q, want %q", registry.Name, "registry")
	}

	const defaultConfigurationPath = "/config.yml"
	configurationPath := defaultConfigurationPath
	hardcodedEnvs := map[string]string{
		"REGISTRY_HTTP_ADDR": ":5000",
		"REGISTRY_HTTP_NET":  "tcp",
	}
	envs := map[string]string{}
	for _, env := range registry.Env {
		if env.ValueFrom != nil {
			// FIXME(dmage): should we support it?
			return fail("environment variable %s: valueFrom is not supported", env.Name)
		}
		if value, ok := hardcodedEnvs[env.Name]; ok {
			if env.Value != value {
				return fail("environment variable %s: got %q, but the only supported value is %q", env.Name, env.Value, value)
			}
		} else if env.Name == "REGISTRY_CONFIGURATION_PATH" {
			configurationPath = env.Value
		} else if isEnvVarSupported(env.Name) {
			envs[env.Name] = env.Value
		} else {
			return fail("unsupported environment variable %s", env.Name)
		}
	}

	// TODO(dmage): registry.EnvFrom?

	podFileGetter := newPodFileGetter(registry, dc.Spec.Template.Spec.Volumes, resources)
	configData, err := podFileGetter.PodFile(configurationPath)
	if _, ok := err.(errVolumeMountNotFound); ok {
		if configurationPath != defaultConfigurationPath {
			return fail("configuration path %q is not default, but no volume mounts found for it", configurationPath)
		}
		configData = []byte(`version: 0.1
storage:
  cache:
    blobdescriptor: inmemory
  filesystem:
    rootdirectory: /registry
  delete:
    enabled: true
`)
	} else if err != nil {
		return fail("unable to get the configuration file: %s", err)
	}

	if _, ok := envs["REGISTRY_OPENSHIFT_SERVER_ADDR"]; !ok {
		envs["REGISTRY_OPENSHIFT_SERVER_ADDR"] = "must-be-set-to-parse-the-config-file"
	}
	for key, value := range envs {
		if err := os.Setenv(key, value); err != nil {
			return fail("unable to set the environment variable %s: %v", key, err)
		}
	}
	config, extraConfig, err := registryconfig.Parse(bytes.NewReader(configData))
	if err != nil {
		return fail("unable to parse the configuration file: %s", err)
	}

	storage, err := newImageRegistryConfigStorage(config, dc, registry)
	if err != nil {
		return fail("unable to make configuration for the storage: %s", err)
	}

	tls, tlsSecret, err := migrateTLS(config, podFileGetter)
	if err != nil {
		return fail("unable to process TLS configuration: %s", err)
	}

	return imageregistryapi.ImageRegistrySpec{
		OperatorSpec: operatorapi.OperatorSpec{
			ManagementState: operatorapi.Managed,
			Version:         "none",
			ImagePullSpec:   registry.Image,
		},
		HTTPSecret: config.HTTP.Secret,
		Proxy:      imageregistryapi.ImageRegistryConfigProxy{}, // TODO
		Storage:    storage,
		Requests: imageregistryapi.ImageRegistryConfigRequests{
			Read: imageregistryapi.ImageRegistryConfigRequestsLimits{
				MaxRunning:     extraConfig.Requests.Read.MaxRunning,
				MaxInQueue:     extraConfig.Requests.Read.MaxInQueue,
				MaxWaitInQueue: extraConfig.Requests.Read.MaxWaitInQueue,
			},
			Write: imageregistryapi.ImageRegistryConfigRequestsLimits{
				MaxRunning:     extraConfig.Requests.Write.MaxRunning,
				MaxInQueue:     extraConfig.Requests.Write.MaxInQueue,
				MaxWaitInQueue: extraConfig.Requests.Write.MaxWaitInQueue,
			},
		},
		TLS:          tls,
		DefaultRoute: false,                                         // TODO
		Routes:       []imageregistryapi.ImageRegistryConfigRoute{}, // TODO
		Replicas:     dc.Spec.Replicas,
	}, tlsSecret, nil
}
