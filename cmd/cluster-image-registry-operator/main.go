package main

import (
	"flag"
	"math/rand"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog"

	"github.com/openshift/library-go/pkg/operator/watchdog"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/controllers"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/version"
)

const metricsPort = 60000

func printVersion() {
	klog.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	klog.Infof("Go Version: %s", runtime.Version())
	klog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func main() {
	cmd := &cobra.Command{
		Use:   "cluster-image-registry-operator",
		Short: "OpenShift cluster image registry operator",
		Run:   runOperator,
	}
	cmd.AddCommand(watchdog.NewFileWatcherWatchdog())
	if err := cmd.Execute(); err != nil {
		klog.Errorf("%v", err)
		os.Exit(1)
	}
}

// runOperator starts image registry operator and is our default command when
// no other parameter is provided on the command line.
func runOperator(cmd *cobra.Command, args []string) {
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
	namespace, err := regopclient.GetWatchNamespace()
	if err != nil {
		klog.Fatalf("failed to get watch namespace: %s", err)
	}

	p := &parameters.Globals{}

	p.Deployment.Namespace = namespace
	p.Deployment.Labels = map[string]string{"docker-registry": "default"}

	p.Pod.ServiceAccount = "registry"
	p.Container.Port = 5000

	p.Healthz.Route = "/healthz"
	p.Healthz.TimeoutSeconds = 5

	p.Service.Name = defaults.ImageRegistryName
	p.ImageConfig.Name = "cluster"
	p.CAConfig.Name = defaults.ImageRegistryCertificatesName
	p.ServiceCA.Name = "serviceca"
	p.TrustedCA.Name = "trusted-ca"

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	g, err := regopclient.NewGenerator(cfg, p, stopCh)
	if err != nil {
		klog.Fatalf("failed to generate clients and listers: %v", err)
	}

	go metrics.RunServer(metricsPort, stopCh)

	clusterOperatorStatusController, err := controllers.NewClusterOperatorStatusController(g)
	if err != nil {
		klog.Fatal(err)
	}
	go clusterOperatorStatusController.Run(stopCh)

	imagePrunerController, err := controllers.NewImagePrunerController(g)
	if err != nil {
		klog.Fatal(err)
	}

	go imagePrunerController.Run(stopCh)

	imageRegistryController, err := controllers.NewController(g)
	if err != nil {
		klog.Fatal(err)
	}

	err = imageRegistryController.Run(stopCh)
	if err != nil {
		klog.Fatal(err)
	}
}
