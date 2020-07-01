package main

import (
	"context"
	"flag"
	"math/rand"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"

	"github.com/openshift/library-go/pkg/operator/watchdog"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/pkg/version"
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
		_ = logstderr.Value.Set("true")
	}

	printVersion()

	rand.Seed(time.Now().UnixNano())

	cfg, err := regopclient.GetConfig()
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err)
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	go metrics.RunServer(metricsPort, stopCh)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		<-stopCh
		klog.Infof("Received SIGTERM or SIGINT signal, shutting down the operator.")
	}()

	workerID, err := os.Hostname()
	if err != nil {
		klog.Fatalf("Error getting hostname: %s", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error creating clientset: %s", err)
	}

	lock, err := resourcelock.New(
		resourcelock.ConfigMapsResourceLock,
		defaults.ImageRegistryOperatorNamespace,
		defaults.LeaderLockConfigMapName,
		kubeClient.CoreV1(),
		kubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: workerID,
		},
	)
	if err != nil {
		klog.Fatalf("Error creating resource lock: %v", err)
	}

	leaderelection.RunOrDie(
		ctx,
		leaderelection.LeaderElectionConfig{
			Lock:          lock,
			LeaseDuration: 15 * time.Second,
			RenewDeadline: 10 * time.Second,
			RetryPeriod:   2 * time.Second,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					if err = operator.RunOperator(ctx, cfg); err != nil {
						klog.Fatal(err)
					}
				},
				OnStoppedLeading: func() {
					klog.Fatalf("leaderelection lost")
				},
			},
		},
	)
}
