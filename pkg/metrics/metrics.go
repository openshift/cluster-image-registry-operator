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
	azurePrimaryKeyCache = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "image_registry_operator_azure_key_cache_requests_total",
			Help: "Number of Azure keys cache accesses (hit and miss)",
		},
		[]string{"result"},
	)
	imageStreamTags = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "image_registry_image_stream_tags_total",
			Help: "Number of image stream tags. Source is either 'imported' or 'pushed'. 'location' label shows if the tag lives in one of the 'openshift' namespaces or 'other'",
		},
		[]string{"source", "location"},
	)
	storageType = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "image_registry_storage_type",
			Help: "Holds the storage in use for the image registry",
		},
		[]string{"storage"},
	)
)

func init() {
	registry.MustRegister(
		storageReconfigured,
		imagePrunerInstallStatus,
		azurePrimaryKeyCache,
		imageStreamTags,
		storageType,
	)
}
