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

var _ Templator = &generatorSecret{}

type generatorSecret struct {
	BaseTemplator
	client *coreset.CoreV1Client
}

func makeSecret(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorSecret{
		BaseTemplator: BaseTemplator{
			Name:      cr.ObjectMeta.Name + "-private-configuration",
			Namespace: g.params.Deployment.Namespace,
			Strategy:  strategy.Secret{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gs *generatorSecret) Expected() (runtime.Object, error) {
	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gs.Name,
			Namespace:   gs.Namespace,
			Annotations: gs.Annotations,
		},
	}

	addOwnerRefToObject(s, asOwner(gs.Generator.ImageRegistry))

	return s, nil
}

func (gs *generatorSecret) Get() (runtime.Object, error) {
	return gs.client.Secrets(gs.Namespace).Get(gs.Name, metav1.GetOptions{})
}

func (gs *generatorSecret) Create() error {
	tmpl, err := gs.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*corev1.Secret)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gs.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gs.client.Secrets(gs.Namespace).Create(n)
	return err
}

func (gs *generatorSecret) Update(current runtime.Object) error {
	tmpl, err := gs.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gs.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gs.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gs.GetTemplateName())
			return nil
		}
	}

	updated, err := gs.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*corev1.Secret)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gs.client.Secrets(gs.Namespace).Update(n)
	return err
}

func (gs *generatorSecret) Delete(opts *metav1.DeleteOptions) error {
	return gs.client.Secrets(gs.Namespace).Delete(gs.Name, opts)
}
