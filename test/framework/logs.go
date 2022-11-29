package framework

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

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

func GetLogsByLabelSelector(client *Clientset, namespace string, labelSelector *metav1.LabelSelector, previous bool) (PodSetLogs, error) {
	ctx := context.Background()

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}

	podList, err := client.Pods(namespace).List(
		ctx, metav1.ListOptions{
			LabelSelector: selector.String(),
		},
	)
	if err != nil {
		return nil, err
	}

	podLogs := make(PodSetLogs)
	for _, pod := range podList.Items {
		podLog, err := readPodLogs(ctx, client, &pod, previous)
		if err != nil {
			return nil, err
		}
		podLogs[pod.Name] = podLog
	}
	return podLogs, nil
}

func GetLogsForPod(ctx context.Context, client *Clientset, namespace string, podName string) (PodSetLogs, error) {
	pod, err := client.Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	podLogs, err := readPodLogs(ctx, client, pod, false)
	if err != nil {
		return nil, err
	}
	podSetLogs := make(PodSetLogs)
	podSetLogs[pod.Name] = podLogs
	return podSetLogs, nil
}

func readPodLogs(ctx context.Context, client *Clientset, pod *corev1.Pod, previous bool) (PodLog, error) {
	podLog := make(PodLog)
	for _, container := range pod.Spec.Containers {
		var containerLog ContainerLog
		log, err := client.Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Container: container.Name,
			Previous:  previous,
		}).Stream(ctx)
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
	return podLog, nil
}

func DumpPodLogs(logger Logger, podLogs PodSetLogs) {
	if len(podLogs) > 0 {
		for pod, containers := range podLogs {
			for container, logs := range containers {
				var buf strings.Builder
				fmt.Fprintf(&buf, "logs for pod/%s (container %s)\n", pod, container)
				for _, line := range logs {
					fmt.Fprintf(&buf, "%s", line)
				}
				logger.Logf("%s", buf.String())
			}
		}
	}
}

// FollowPodLog attaches to the pod log stream, reads it until the pod is dead
// or an error happens while reading.
//
// If an error happens when fetching pod's Stream() this function returns
// immediately. If a failure happens during pods log read the error is sent back
// to the caller through an error channel.
func FollowPodLog(client *Clientset, pod corev1.Pod) (<-chan string, <-chan error, error) {
	ls, err := client.Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Follow: true,
	}).Stream(context.Background())
	if err != nil {
		return nil, nil, err
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
	return logch, errch, nil
}
