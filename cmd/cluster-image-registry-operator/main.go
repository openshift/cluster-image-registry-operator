package main

import (
	"flag"
	"os"
	"runtime"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	appsset "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/version"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func printVersion() {
	klog.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	klog.Infof("Go Version: %s", runtime.Version())
	klog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func main() {
	flag.Parse()

	printVersion()

	cfg, err := client.GetConfig()
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err)
	}

	namespace, err := client.GetWatchNamespace()
	if err != nil {
		klog.Fatalf("failed to get watch namespace: %s", err)
	}

	if envval := os.Getenv("IMAGE_REGISTRY_OPERATOR_NO_ADOPTION"); envval == "" {
		client, err := appsset.NewForConfig(cfg)
		if err != nil {
			klog.Fatal(err)
		}

		_, err = client.DeploymentConfigs("default").Get("docker-registry", metav1.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				klog.Fatal(err)
			}
		} else {
			namespace = "default"
		}
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	controller, err := operator.NewController(cfg, namespace)
	if err != nil {
		klog.Fatal(err)
	}

	err = controller.Run(stopCh)
	if err != nil {
		klog.Fatal(err)
	}
}
