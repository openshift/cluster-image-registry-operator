package main

import (
	"flag"
	"math/rand"
	"runtime"
	"time"

	"github.com/golang/glog"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/version"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func printVersion() {
	glog.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	glog.Infof("Go Version: %s", runtime.Version())
	glog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func main() {
	flag.Parse()
	flag.Lookup("logtostderr").Value.Set("true")

	printVersion()

	rand.Seed(time.Now().UnixNano())

	cfg, err := regopclient.GetConfig()
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err)
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	controller, err := operator.NewController(cfg)
	if err != nil {
		glog.Fatal(err)
	}

	err = controller.Run(stopCh)
	if err != nil {
		glog.Fatal(err)
	}
}
