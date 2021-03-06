package resourceapply

import (
	"context"

	"github.com/imdario/mergo"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/openshift/library-go/pkg/operator/events"
)

var serviceMonitorGVR = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}

func ensureGenericSpec(required, existing *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	requiredSpec, _, err := unstructured.NestedMap(required.UnstructuredContent(), "spec")
	if err != nil {
		return nil, false, err
	}
	existingSpec, _, err := unstructured.NestedMap(existing.UnstructuredContent(), "spec")
	if err != nil {
		return nil, false, err
	}

	if err := mergo.Merge(&existingSpec, &requiredSpec); err != nil {
		return nil, false, err
	}

	if equality.Semantic.DeepEqual(existingSpec, requiredSpec) {
		return existing, false, nil
	}

	existingCopy := existing.DeepCopy()
	if err := unstructured.SetNestedMap(existingCopy.UnstructuredContent(), existingSpec, "spec"); err != nil {
		return nil, true, err
	}

	return existingCopy, true, nil
}

// ApplyServiceMonitor applies the Prometheus service monitor.
func ApplyServiceMonitor(client dynamic.Interface, recorder events.Recorder, required *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	namespace := required.GetNamespace()

	existing, err := client.Resource(serviceMonitorGVR).Namespace(namespace).Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		newObj, createErr := client.Resource(serviceMonitorGVR).Namespace(namespace).Create(context.TODO(), required, metav1.CreateOptions{})
		if createErr != nil {
			recorder.Warningf("ServiceMonitorCreateFailed", "Failed to create ServiceMonitor.monitoring.coreos.com/v1: %v", createErr)
			return nil, true, createErr
		}
		recorder.Eventf("ServiceMonitorCreated", "Created ServiceMonitor.monitoring.coreos.com/v1 because it was missing")
		return newObj, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()

	updated, endpointsModified, err := ensureGenericSpec(required, existingCopy)
	if err != nil {
		return nil, false, err
	}

	if !endpointsModified {
		return nil, false, nil
	}

	if klog.V(4).Enabled() {
		klog.Infof("ServiceMonitor %q changes: %v", namespace+"/"+required.GetName(), JSONPatchNoError(existing, existingCopy))
	}

	newObj, err := client.Resource(serviceMonitorGVR).Namespace(namespace).Update(context.TODO(), updated, metav1.UpdateOptions{})
	if err != nil {
		recorder.Warningf("ServiceMonitorUpdateFailed", "Failed to update ServiceMonitor.monitoring.coreos.com/v1: %v", err)
		return nil, true, err
	}

	recorder.Eventf("ServiceMonitorUpdated", "Updated ServiceMonitor.monitoring.coreos.com/v1 because it changed")
	return newObj, true, err
}

var prometheusRuleGVR = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "prometheusrules"}

// ApplyPrometheusRule applies the PrometheusRule
func ApplyPrometheusRule(client dynamic.Interface, recorder events.Recorder, required *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	namespace := required.GetNamespace()

	existing, err := client.Resource(prometheusRuleGVR).Namespace(namespace).Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		newObj, createErr := client.Resource(prometheusRuleGVR).Namespace(namespace).Create(context.TODO(), required, metav1.CreateOptions{})
		if createErr != nil {
			recorder.Warningf("PrometheusRuleCreateFailed", "Failed to create PrometheusRule.monitoring.coreos.com/v1: %v", createErr)
			return nil, true, createErr
		}
		recorder.Eventf("PrometheusRuleCreated", "Created PrometheusRule.monitoring.coreos.com/v1 because it was missing")
		return newObj, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()

	updated, endpointsModified, err := ensureGenericSpec(required, existingCopy)
	if err != nil {
		return nil, false, err
	}

	if !endpointsModified {
		return nil, false, nil
	}

	if klog.V(4).Enabled() {
		klog.Infof("PrometheusRule %q changes: %v", namespace+"/"+required.GetName(), JSONPatchNoError(existing, existingCopy))
	}

	newObj, err := client.Resource(prometheusRuleGVR).Namespace(namespace).Update(context.TODO(), updated, metav1.UpdateOptions{})
	if err != nil {
		recorder.Warningf("PrometheusRuleUpdateFailed", "Failed to update PrometheusRule.monitoring.coreos.com/v1: %v", err)
		return nil, true, err
	}

	recorder.Eventf("PrometheusRuleUpdated", "Updated PrometheusRule.monitoring.coreos.com/v1 because it changed")
	return newObj, true, err
}
