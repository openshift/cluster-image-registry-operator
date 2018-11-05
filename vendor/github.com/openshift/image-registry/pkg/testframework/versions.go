package testframework

import (
	"os"
)

var (
	// originImageRef is the Docker image name of the OpenShift Origin container.
	originImageRef = "docker.io/openshift/origin:latest"
)

func init() {
	version := os.Getenv("ORIGIN_VERSION")
	if len(version) != 0 {
		originImageRef = "docker.io/openshift/origin:" + version
	}
}
