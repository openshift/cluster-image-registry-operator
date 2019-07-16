package main

import (
	"flag"
	"math/rand"
	"runtime"
	"time"

	"k8s.io/klog"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
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
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	logstderr := klogFlags.Lookup("logtostderr")
	if logstderr != nil {
		logstderr.Value.Set("true")
	}

	printVersion()

	rand.Seed(time.Now().UnixNano())

	cfg, err := regopclient.GetConfig()
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err)
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	controller, err := operator.NewController(cfg)
	if err != nil {
		klog.Fatal(err)
	}

	err = controller.Run(stopCh)
	if err != nil {
		klog.Fatal(err)
	}
}
