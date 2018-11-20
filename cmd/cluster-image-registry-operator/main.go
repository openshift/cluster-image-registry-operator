package main

import (
	"flag"
	"os"
	"runtime"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsset "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/version"

	"github.com/sirupsen/logrus"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

var (
	logLevel string
)

func init() {
	flag.StringVar(&logLevel, "loglevel", "", "sets the sensitivity of logging output.")
}

func printVersion() {
	logrus.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	logrus.Infof("Go Version: %s", runtime.Version())
	logrus.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func main() {
	flag.Parse()

	if len(logLevel) == 0 {
		envval := os.Getenv("LOG_LEVEL")
		if len(envval) > 0 {
			logLevel = envval
		} else {
			logLevel = "info"
		}
	}
	lvl, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logrus.Fatal(err)
	}

	logrus.SetLevel(lvl)

	cfg, err := client.GetConfig()
	if err != nil {
		logrus.Fatalf("Error building kubeconfig: %s", err)
	}

	printVersion()

	namespace, err := client.GetWatchNamespace()
	if err != nil {
		logrus.Fatalf("failed to get watch namespace: %s", err)
	}

	if envval := os.Getenv("IMAGE_REGISTRY_OPERATOR_NO_ADOPTION"); envval == "" {
		client, err := appsset.NewForConfig(cfg)
		if err != nil {
			logrus.Fatal(err)
		}

		_, err = client.DeploymentConfigs("default").Get("docker-registry", metav1.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				logrus.Fatal(err)
			}
		} else {
			namespace = "default"
		}
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	controller, err := operator.NewController(cfg, namespace)
	if err != nil {
		logrus.Fatal(err)
	}

	err = controller.Run(stopCh)
	if err != nil {
		logrus.Fatal(err)
	}
}
