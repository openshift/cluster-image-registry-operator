package migration

import (
	"fmt"

	coreapi "k8s.io/api/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/coreutil"
	"github.com/openshift/cluster-image-registry-operator/pkg/migration/dependency"
)

type PodFileGetter interface {
	PodFile(filename string) ([]byte, error)
}

func newPodFileGetter(container coreapi.Container, volumes []coreapi.Volume, resources dependency.NamespacedResources) PodFileGetter {
	return &defaultPodFileGetter{
		container: container,
		volumes:   volumes,
		resources: resources,
	}
}

type defaultPodFileGetter struct {
	container coreapi.Container
	volumes   []coreapi.Volume
	resources dependency.NamespacedResources
}

func (pf *defaultPodFileGetter) PodFile(filename string) ([]byte, error) {
	volumeMount, fileKey, err := getVolumeMount(pf.container.VolumeMounts, filename)
	if _, ok := err.(errVolumeMountNotFound); ok {
		return nil, err
	}

	volumeSource, ok := getVolumeSource(pf.volumes, volumeMount.Name)
	if !ok {
		return nil, fmt.Errorf("unable to get file %q: the volume source %s is not defined", filename, volumeMount.Name)
	}

	volumeSourceType, err := coreutil.GetVolumeSourceField(volumeSource)
	if err != nil {
		return nil, fmt.Errorf("unable to get file %q: %s", filename, err)
	}

	switch volumeSourceType.Name {
	case "Secret":
		if len(volumeSource.Secret.Items) != 0 {
			return nil, fmt.Errorf("unable to get file %q: volume source items are not supported", filename)
		}

		secret, err := pf.resources.Secret(volumeSource.Secret.SecretName)
		if err != nil {
			return nil, fmt.Errorf("unable to get file %q: get secret %s: %s", filename, volumeSource.Secret.SecretName, err)
		}
		data, ok := secret.Data[fileKey]
		if !ok {
			return nil, fmt.Errorf("unable to get file %q: the secret %s does not have a value for %q", filename, volumeSource.Secret.SecretName, fileKey)
		}
		return data, nil
	case "ConfigMap":
		if len(volumeSource.ConfigMap.Items) != 0 {
			return nil, fmt.Errorf("unable to get file %q: volume source items are not supported", filename)
		}

		configMap, err := pf.resources.ConfigMap(volumeSource.ConfigMap.Name)
		if err != nil {
			return nil, fmt.Errorf("unable to get file %q: get config map %s: %s", filename, volumeSource.ConfigMap.Name, err)
		}
		// TODO(dmage): BinaryData?
		data, ok := configMap.Data[fileKey]
		if !ok {
			return nil, fmt.Errorf("unable to get file %q: the config map %s does not have a value for %q", filename, volumeSource.ConfigMap.Name, fileKey)
		}
		return []byte(data), nil
	default:
		return nil, fmt.Errorf("unable to get file %q: the volume source type %s is not supported", filename, volumeSourceType.Name)
	}
}
