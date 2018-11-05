package migration

import (
	"fmt"
	"strings"

	coreapi "k8s.io/api/core/v1"
)

type errVolumeMountNotFound struct {
	filename string
}

func (e errVolumeMountNotFound) Error() string {
	return fmt.Sprintf("no volume mounts found for %q", e.filename)
}

func getVolumeMount(volumeMounts []coreapi.VolumeMount, filename string) (coreapi.VolumeMount, string, error) {
	if !strings.HasPrefix(filename, "/") {
		return coreapi.VolumeMount{}, "", fmt.Errorf("cannot find a volume mount for the relative path %q", filename)
	}

	var bestVolumeMount coreapi.VolumeMount
	var key string
	for _, volumeMount := range volumeMounts {
		if len(bestVolumeMount.MountPath) > len(volumeMount.MountPath) {
			continue
		}
		match := false
		if strings.HasSuffix(volumeMount.MountPath, "/") {
			match = strings.HasPrefix(filename, volumeMount.MountPath)
		} else {
			match = filename == volumeMount.MountPath || strings.HasPrefix(filename, volumeMount.MountPath+"/")
		}
		if match {
			bestVolumeMount = volumeMount

			key = filename[len(volumeMount.MountPath):]
			if strings.HasPrefix(key, "/") {
				key = key[1:]
			}

			subpath := volumeMount.SubPath
			if subpath != "" && !strings.HasSuffix(subpath, "/") && key != "" {
				subpath += "/"
			}
			key = subpath + key
		}
	}
	if bestVolumeMount.MountPath == "" {
		return bestVolumeMount, key, errVolumeMountNotFound{filename: filename}
	}
	return bestVolumeMount, key, nil
}
