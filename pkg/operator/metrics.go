package operator

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	imagev1 "github.com/openshift/api/image/v1"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions/image/v1"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
)

// MetricsController is a controller that runs from time to time and reports some metrics about
// the current status of the system.
type MetricsController struct {
	lister imagelisters.ImageStreamLister
	caches []cache.InformerSynced
}

// NewMetricsController returns a new MetricsController.
func NewMetricsController(informer imageinformers.ImageStreamInformer) *MetricsController {
	return &MetricsController{
		lister: informer.Lister(),
		caches: []cache.InformerSynced{informer.Informer().HasSynced},
	}
}

// report gathers all metrics reported by this operator and calls appropriate function in the
// metrics package to report the current values.
func (m *MetricsController) report(_ context.Context) {
	imgstreams, err := m.lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("unable to list image streams: %s", err)
		return
	}

	var importedOpenShift float64
	var pushedOpenShift float64
	var importedOther float64
	var pushedOther float64
	for _, is := range imgstreams {
		imported, pushed := m.assessImageStream(is.DeepCopy())
		if strings.HasPrefix(is.Namespace, "openshift") {
			importedOpenShift += imported
			pushedOpenShift += pushed
			continue
		}

		importedOther += imported
		pushedOther += pushed
	}

	metrics.ReportOpenShiftImageStreamTags(importedOpenShift, pushedOpenShift)
	metrics.ReportOtherImageStreamTags(importedOther, pushedOther)
}

// assessImageStream returns the number of imported and the number of pushed tags for the provided
// image stream reference.
func (m *MetricsController) assessImageStream(is *imagev1.ImageStream) (float64, float64) {
	spectags := map[string]bool{}
	for _, tag := range is.Spec.Tags {
		spectags[tag.Name] = true
	}

	var imported float64
	var pushed float64
	for _, tag := range is.Status.Tags {
		if _, ok := spectags[tag.Tag]; ok {
			imported++
		} else {
			pushed++
		}
	}

	return imported, pushed
}

// Run starts this controller. Runs the main loop in a separate go routine and bails out when
// the provided context is finished.
func (m *MetricsController) Run(ctx context.Context) {
	klog.Infof("Starting MetricsController")
	if !cache.WaitForCacheSync(ctx.Done(), m.caches...) {
		return
	}

	go wait.UntilWithContext(ctx, m.report, time.Hour)
	klog.Infof("Started MetricsController")
	<-ctx.Done()
	klog.Infof("Shutting down MetricsController")
}
