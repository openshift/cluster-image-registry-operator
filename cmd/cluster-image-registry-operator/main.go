package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/pkg/version"
)

const metricsPort = 60000

var (
	filesToWatch []string
)

func printVersion() {
	klog.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	klog.Infof("Go Version: %s", runtime.Version())
	klog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func main() {
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	if logstderr := klogFlags.Lookup("logtostderr"); logstderr != nil {
		_ = logstderr.Value.Set("true")
	}

	watchedFileChanged := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	stopCh := signals.SetupSignalHandler()
	go func() {
		defer cancel()
		select {
		case <-stopCh:
			klog.Infof("Received SIGTERM or SIGINT signal, shutting down the operator.")
		case <-watchedFileChanged:
			klog.Infof("Watched file changed, shutting down the operator.")
		}
	}()

	cmd := &cobra.Command{
		Use:   "cluster-image-registry-operator",
		Short: "OpenShift cluster image registry operator",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl := controllercmd.NewController(
				"image-registry-operator",
				func(ctx context.Context, cctx *controllercmd.ControllerContext) error {
					printVersion()
					klog.Infof("Watching files %v...", filesToWatch)
					rand.Seed(time.Now().UnixNano())
					go metrics.RunServer(metricsPort)
					return operator.RunOperator(ctx, *cctx)
				},
			).WithLeaderElection(
				configv1.LeaderElection{},
				defaults.ImageRegistryOperatorNamespace,
				"openshift-master-controllers",
			).WithRestartOnChange(
				watchedFileChanged, nil, filesToWatch...,
			)

			if err := ctrl.Run(ctx, nil); err != nil {
				log.Fatal(err)
			}
		},
	}

	cmd.Flags().StringArrayVar(&filesToWatch, "files", []string{}, "List of files to watch")

	if err := cmd.Execute(); err != nil {
		klog.Errorf("%v", err)
		os.Exit(1)
	}
}
