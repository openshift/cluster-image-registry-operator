package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	registry            = prometheus.NewRegistry()
	storageReconfigured = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "image_registry_operator",
		Subsystem: "storage",
		Name:      "reconfigured_total",
		Help:      "Total times the image registry's storage was reconfigured.",
	})
	degraded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "image_registry_operator",
		Subsystem: "status",
		Name:      "degraded_total",
		Help:      "Total times the image registry operator reported itself `Degraded`, per reason.",
	}, []string{"reason"})
)

func init() {
	registry.MustRegister(
		storageReconfigured,
		degraded,
	)
}
