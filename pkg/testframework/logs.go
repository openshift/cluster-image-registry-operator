package testframework

import (
	"bufio"
	"fmt"
	"io"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PodLog []string

func (log PodLog) Contains(re *regexp.Regexp) bool {
	for _, line := range log {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

type PodSetLogs map[string]PodLog

func (psl PodSetLogs) Contains(re *regexp.Regexp) bool {
	for _, podlog := range psl {
		if podlog.Contains(re) {
			return true
		}
	}
	return false
}

func GetLogsByLabelSelector(client *Clientset, namespace string, labelSelector *metav1.LabelSelector) (PodSetLogs, error) {
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

	podLogs := make(PodSetLogs)
	for _, pod := range podList.Items {
		var podLog PodLog
		log, err := client.Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{}).Stream()
		if err != nil {
			return nil, fmt.Errorf("failed to get logs for pod %s: %s", pod.Name, err)
		}
		r := bufio.NewReader(log)
		for {
			line, readErr := r.ReadSlice('\n')
			if len(line) > 0 || readErr == nil {
				podLog = append(podLog, string(line))
			}
			if readErr == io.EOF {
				break
			} else if readErr != nil {
				return nil, fmt.Errorf("failed to read log for pod %s: %s", pod.Name, readErr)
			}
		}
		podLogs[pod.Name] = podLog
	}
	return podLogs, nil
}

func DumpPodLogs(logger Logger, podLogs PodSetLogs) {
	if len(podLogs) > 0 {
		for pod, logs := range podLogs {
			logger.Logf("=== logs for pod/%s", pod)
			for _, line := range logs {
				logger.Logf("%s", line)
			}
		}
		logger.Logf("=== end of logs")
	}
}
