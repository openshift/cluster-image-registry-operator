package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	"github.com/openshift/cluster-image-registry-operator/pkg/nodeca"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/pkg/version"
)

var (
	controllerConfig string
	kubeconfig       string
	filesToWatch     []string
)

func printVersion() {
	klog.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	klog.Infof("Go Version: %s", runtime.Version())
	klog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}

// readAndParseControllerConfig reads and parses the controller configuration file.
// If path is empty, returns default config (for backwards compatibility during
// migration to file-based configuration).
func readAndParseControllerConfig(path string) (*configv1.GenericControllerConfig, error) {
	config := &configv1.GenericControllerConfig{
		ServingInfo: configv1.HTTPServingInfo{
			ServingInfo: configv1.ServingInfo{
				BindAddress: ":60000",
			},
		},
	}
	if path == "" {
		return config, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := kubeyaml.Unmarshal(content, config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config content: %w", err)
	}

	// make sure we always have a bind address present in the config. This
	// is just for the case where the config has an explicitly empty
	// bindAddress value.
	if config.ServingInfo.BindAddress == "" {
		config.ServingInfo.BindAddress = ":60000"
	}

	return config, nil
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
					config, err := readAndParseControllerConfig(controllerConfig)
					if err != nil {
						return fmt.Errorf("failed to read config: %w", err)
					}

					klog.Infof("Watching files %v...", filesToWatch)

					metricsServer := metrics.NewServer(
						"/etc/secrets/tls.crt",
						"/etc/secrets/tls.key",
						config.ServingInfo,
					)

					if err := metricsServer.Run(); err != nil {
						return fmt.Errorf("failed to run metrics server: %w", err)
					}

					return operator.RunOperator(ctx, cctx.KubeConfig)
				},
				clock.RealClock{},
			).WithKubeConfigFile(
				kubeconfig, nil,
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

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster")
	cmd.Flags().StringArrayVar(&filesToWatch, "files", []string{}, "List of files to watch")
	cmd.Flags().StringVar(&controllerConfig, "config", "", "Path to the controller config file")

	cmd.AddCommand(
		&cobra.Command{
			Use:   "node-ca-sync",
			Short: "Runs the node-ca certificate syncer",
			Long:  "Runs a daemon that keeps /etc/docker/certs.d in sync with /tmp/serviceca",
			Run: func(cmd *cobra.Command, args []string) {
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()

				ticker := time.NewTicker(time.Minute)
				for {
					if copied, skipped, trimmed, err := nodeca.SyncCerts(
						"/tmp/serviceca", "/etc/docker/certs.d",
					); err != nil {
						klog.Errorf("syncing certs: %v", err)
					} else {
						klog.Infof("copied: %03d skipped: %03d trimmed: %03d", copied, skipped, trimmed)
					}

					select {
					case <-ticker.C:
					case <-ctx.Done():
						ticker.Stop()
						klog.Info("shutting down node-ca")
						return
					}
				}
			},
		},
	)

	if err := cmd.Execute(); err != nil {
		klog.Errorf("%v", err)
		os.Exit(1)
	}
}
