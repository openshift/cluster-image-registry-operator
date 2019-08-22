package framework

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ContainerLog []string

func (log ContainerLog) Contains(re *regexp.Regexp) bool {
	for _, line := range log {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

type PodLog map[string]ContainerLog

func (log PodLog) Contains(re *regexp.Regexp) bool {
	for _, containerLog := range log {
		if containerLog.Contains(re) {
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
		podLog := make(PodLog)
		for _, container := range pod.Spec.Containers {
			var containerLog ContainerLog
			log, err := client.Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
			}).Stream()
			if err != nil {
				return nil, fmt.Errorf("failed to get logs for pod %s: %s", pod.Name, err)
			}
			r := bufio.NewReader(log)
			for {
				line, readErr := r.ReadString('\n')
				if len(line) > 0 || readErr == nil {
					containerLog = append(containerLog, line)
				}
				if readErr == io.EOF {
					break
				} else if readErr != nil {
					return nil, fmt.Errorf("failed to read log for pod %s: %s", pod.Name, readErr)
				}
			}
			podLog[container.Name] = containerLog
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

// MustFollowPodLog attaches to the pod log stream, reads it until the pod is
// dead or an error happens while reading.
//
// If an error happens when fetching pod's Stream() this function calls t.Fatal
// while if a failure happens during pods log read the error is sent back to
// the caller through an error channel.
func MustFollowPodLog(t *testing.T, pod corev1.Pod) (<-chan string, <-chan error) {
	client := MustNewClientset(t, nil)
	ls, err := client.Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Follow: true,
	}).Stream()
	if err != nil {
		t.Fatal(err)
	}

	logch := make(chan string, 100)
	errch := make(chan error)
	go func() {
		defer close(errch)
		defer close(logch)
		defer ls.Close()

		r := bufio.NewReader(ls)
		for {
			line, err := r.ReadString('\n')
			switch {
			case err == io.EOF:
				return
			case err != nil:
				errch <- err
				return
			default:
				logch <- line
			}
		}
	}()
	return logch, errch
}
