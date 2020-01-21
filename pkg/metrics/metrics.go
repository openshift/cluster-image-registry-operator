package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	registry            = prometheus.NewRegistry()
	storageReconfigured = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "image_registry_operator_storage_reconfigured_total",
		Help: "Total times the image registry's storage was reconfigured.",
	})
	imagePrunerInstallStatus = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "image_registry_operator_image_pruner_install_status",
		Help: "Installation status code related to the automatic image pruning feature. 0 = not installed, 1 = suspended, 2 = enabled",
	})
)

func init() {
	registry.MustRegister(
		storageReconfigured,
		imagePrunerInstallStatus,
	)
}
