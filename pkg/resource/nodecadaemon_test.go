package resource

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	imageregistryfake "github.com/openshift/client-go/imageregistry/clientset/versioned/fake"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/events"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func findToleration(list []corev1.Toleration, cond func(toleration corev1.Toleration) bool) *corev1.Toleration {
	for i, t := range list {
		if cond(t) {
			return &list[i]
		}
	}
	return nil
}

func TestNodeCADaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	imageregistryObjects := []runtime.Object{
		&imageregistryv1.Config{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
		},
	}

	clientset := kfake.NewSimpleClientset()
	imageregistryClient := imageregistryfake.NewSimpleClientset(imageregistryObjects...)

	imageregistryInformers := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, time.Minute)

	operatorClient := client.NewConfigOperatorClient(
		imageregistryClient.ImageregistryV1().Configs(),
		imageregistryInformers.Imageregistry().V1().Configs(),
	)

	imageregistryInformers.Start(ctx.Done())
	imageregistryInformers.WaitForCacheSync(ctx.Done())

	g := NewGeneratorNodeCADaemonSet(events.NewInMemoryRecorder("image-registry-operator"), nil, nil, clientset.AppsV1(), operatorClient)
	obj, err := g.Create()
	if err != nil {
		t.Fatal(err)
	}

	ds := obj.(*appsv1.DaemonSet)
	noScheduleToleration := findToleration(ds.Spec.Template.Spec.Tolerations, func(tol corev1.Toleration) bool {
		return tol.Key == "" && tol.Operator == "Exists" && tol.Value == "" && tol.Effect == ""
	})
	if noScheduleToleration == nil {
		t.Errorf("unable to find toleration for all taints, %#+v", ds.Spec.Template.Spec.Tolerations)
	}
}
