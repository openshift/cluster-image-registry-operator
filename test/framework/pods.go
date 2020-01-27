package framework

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getPodsByLabelSelector(client *Clientset, namespace string, labelSelector *metav1.LabelSelector) ([]corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}

	podList, err := client.Pods(namespace).List(metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}
