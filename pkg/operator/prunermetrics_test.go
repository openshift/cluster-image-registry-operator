package operator

import (
	"context"
	"crypto/tls"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"os"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	prometheusutil "github.com/openshift/cluster-image-registry-operator/test/util/prometheus"
)

func TestMain(m *testing.M) {
	// sets the default http client to skip certificate check.
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	ch := make(chan struct{})
	tlsKey, tlsCRT, err := metrics.StartTestMetricsServer(5000, ch)
	if err != nil {
		panic(err)
	}

	// give http handlers/server some time to process certificates and
	// get online before running tests.
	time.Sleep(time.Second)

	code := m.Run()
	os.Remove(tlsKey)
	os.Remove(tlsCRT)
	close(ch)
	os.Exit(code)
}

func TestPrunerCompletedJobsMetric(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	fakeClient := fake.NewSimpleClientset()
	controller := newTestMetricsController(fakeClient)
	controller.start(stopCh)
	defer controller.stop()

	jobsMetricName := "image_registry_operator_image_pruner_completed_jobs_total"

	// create job
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "pruner", Namespace: "test-ns"}}
	job, err := fakeClient.BatchV1().Jobs("test-ns").Create(job)
	if err != nil {
		t.Fatalf("failed to create job %q: %v", "pruner", err)
	}
	checkCounterValue(jobsMetricName, "succeeded", 0, t)
	// mark job as completed. successful
	now := metav1.Now()
	job.Status.CompletionTime = &now
	job.Status.Succeeded = 1
	_, err = fakeClient.BatchV1().Jobs("test-ns").UpdateStatus(job)
	// Need to wait just a little bit for the controller to update the data.
	time.Sleep(10 * time.Millisecond)
	checkCounterValue(jobsMetricName, "succeeded", 1, t)

	failedJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: "test-ns"}}
	failedJob, err = fakeClient.BatchV1().Jobs("test-ns").Create(failedJob)
	if err != nil {
		t.Fatalf("failed to create job %q : %v", "failed", err)
	}
	failedJob.Status.CompletionTime = &now
	failedJob.Status.Failed = 1
	_, err = fakeClient.BatchV1().Jobs("test-ns").UpdateStatus(failedJob)
	// Need to wait just a little bit for the controller to update the data.
	time.Sleep(10 * time.Millisecond)
	checkCounterValue(jobsMetricName, "failed", 1, t)
}

func TestPrunerInstallMetric(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	fakeClient := fake.NewSimpleClientset()
	controller := newTestMetricsController(fakeClient)
	controller.start(stopCh)
	defer controller.stop()

	metricName := "image_registry_operator_image_pruner_install_status"

	// no cronJob, should be 0
	checkGaugeValue(metricName, 0, t)

	// add CronJob, should be 2
	cronJob := &batchv1beta1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-pruner",
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Spec: batchv1beta1.CronJobSpec{
			Schedule: "*/1 * * *",
		},
	}
	cronJob, err := fakeClient.BatchV1beta1().CronJobs(defaults.ImageRegistryOperatorNamespace).Create(cronJob)
	if err != nil {
		t.Fatalf("failed to create CronJob: %v", err)
	}
	// Need to wait just a little bit for the controller to update the data.
	time.Sleep(10 * time.Millisecond)
	checkGaugeValue(metricName, 2, t)

	// Suspend CronJob, should be 1
	truePtr := true
	cronJob.Spec.Suspend = &truePtr
	cronJob, err = fakeClient.BatchV1beta1().CronJobs(defaults.ImageRegistryOperatorNamespace).Update(cronJob)
	if err != nil {
		t.Fatalf("failed to update CronJob: %v", err)
	}
	// Need to wait just a little bit for the controller to update the data.
	time.Sleep(10 * time.Millisecond)
	checkGaugeValue(metricName, 1, t)

	// Delete CronJob, should be back to 0
	err = fakeClient.BatchV1beta1().CronJobs(defaults.ImageRegistryOperatorNamespace).Delete(cronJob.Name, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("failed to delete CronJob: %v", err)
	}
	// Need to wait just a little bit for the controller to update the data.
	time.Sleep(10 * time.Millisecond)
	checkGaugeValue(metricName, 0, t)
}

func checkCounterValue(metric string, label string, expected float64, t *testing.T) {
	total, _, err := getValueForCounterVec(metric, []string{label})
	if err != nil {
		t.Fatalf("failed to get value for %s: %v", metric, err)
	}
	if total != expected {
		t.Errorf("expected %f for metric %s{%s}, got %f", expected, metric, label, total)
	}
}

func getValueForCounterVec(metricName string, labels []string) (float64, bool, error) {
	metrics, err := prometheusutil.GetMetricsWithName("https://localhost:5000/metrics", metricName)
	if err != nil {
		return 0, false, err
	}
	if len(metrics) == 0 {
		return 0, false, nil
	}
	var value float64 = 0
	for _, m := range metrics {
		metricLabels := m.GetLabel()
		labelsMatch := true
		for i, val := range labels {
			if metricLabels[i].GetValue() != val {
				labelsMatch = false
				break
			}
		}
		if labelsMatch {
			value += m.Counter.GetValue()
		}
	}
	return value, true, nil
}

func checkGaugeValue(metricName string, expected float64, t *testing.T) {
	value, _, err := getValueForGauge(metricName)
	if err != nil {
		t.Fatalf("failed to get value for %s: %v", metricName, err)
	}
	if value != expected {
		t.Errorf("expected %s to be %f, got %f", metricName, expected, value)
	}
}

func getValueForGauge(metricName string) (float64, bool, error) {
	metrics, err := prometheusutil.GetMetricsWithName("https://localhost:5000/metrics", metricName)
	if err != nil {
		return 0, false, err
	}
	if len(metrics) == 0 {
		return 0, false, nil
	}
	var value float64 = 0
	for _, m := range metrics {
		value += m.Gauge.GetValue()
	}
	return value, true, nil
}

type testMetricsController struct {
	ctrl            *PrunerMetricsController
	ctx             context.Context
	cancel          context.CancelFunc
	informers       informers.SharedInformerFactory
	jobInformer     cache.SharedIndexInformer
	cronJobInformer cache.SharedIndexInformer
}

func newTestMetricsController(client kubernetes.Interface) *testMetricsController {
	informers := informers.NewSharedInformerFactory(client, 10*time.Minute)
	jobInformer := informers.Batch().V1().Jobs().Informer()
	cronJobInformer := informers.Batch().V1beta1().CronJobs().Informer()
	controller := NewPrunerMetricsController(informers)
	return &testMetricsController{
		ctrl:            controller,
		cronJobInformer: cronJobInformer,
		jobInformer:     jobInformer,
		informers:       informers,
	}
}

func (t *testMetricsController) start(stopCh <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	t.ctx = ctx
	t.cancel = cancel
	t.informers.Start(ctx.Done())
	cache.WaitForCacheSync(stopCh, t.jobInformer.HasSynced, t.cronJobInformer.HasSynced)
	go t.ctrl.Run(stopCh)
}

func (t *testMetricsController) stop() {
	t.cancel()
}
