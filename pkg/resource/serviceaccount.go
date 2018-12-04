package resource

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/golang/glog"
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Templator = &generatorServiceAccount{}

type generatorServiceAccount struct {
	BaseTemplator
	client *coreset.CoreV1Client
}

func makeServiceAccount(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorServiceAccount{
		BaseTemplator: BaseTemplator{
			Name:      g.params.Pod.ServiceAccount,
			Namespace: g.params.Deployment.Namespace,
			Strategy:  strategy.Override{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gsa *generatorServiceAccount) Expected() (runtime.Object, error) {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gsa.Name,
			Namespace:   gsa.Namespace,
			Annotations: gsa.Annotations,
		},
	}

	addOwnerRefToObject(sa, asOwner(gsa.Generator.ImageRegistry))

	return sa, nil
}

func (gsa *generatorServiceAccount) Get() (runtime.Object, error) {
	return gsa.client.ServiceAccounts(gsa.Namespace).Get(gsa.Name, metav1.GetOptions{})
}

func (gsa *generatorServiceAccount) Create() error {
	tmpl, err := gsa.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*corev1.ServiceAccount)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gsa.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gsa.client.ServiceAccounts(gsa.Namespace).Create(n)
	return err
}

func (gsa *generatorServiceAccount) Update(current runtime.Object) error {
	tmpl, err := gsa.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gsa.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gsa.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gsa.GetTemplateName())
			return nil
		}
	}

	updated, err := gsa.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*corev1.ServiceAccount)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gsa.client.ServiceAccounts(gsa.Namespace).Update(n)
	return err
}

func (gsa *generatorServiceAccount) Delete(opts *metav1.DeleteOptions) error {
	return gsa.client.ServiceAccounts(gsa.Namespace).Delete(gsa.Name, opts)
}
