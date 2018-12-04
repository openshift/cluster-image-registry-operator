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

var _ Templator = &generatorConfigMap{}

type generatorConfigMap struct {
	BaseTemplator
	client *coreset.CoreV1Client
}

func makeConfigMap(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorConfigMap{
		BaseTemplator: BaseTemplator{
			Name:      cr.ObjectMeta.Name + "-certificates",
			Namespace: g.params.Deployment.Namespace,
			Strategy:  strategy.ConfigMap{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gcm *generatorConfigMap) Expected() (runtime.Object, error) {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gcm.Name,
			Namespace:   gcm.Namespace,
			Annotations: gcm.Annotations,
		},
	}

	addOwnerRefToObject(cm, asOwner(gcm.Generator.ImageRegistry))

	return cm, nil
}

func (gcm *generatorConfigMap) Get() (runtime.Object, error) {
	return gcm.client.ConfigMaps(gcm.Namespace).Get(gcm.Name, metav1.GetOptions{})
}

func (gcm *generatorConfigMap) Create() error {
	tmpl, err := gcm.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*corev1.ConfigMap)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gcm.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gcm.client.ConfigMaps(gcm.Namespace).Create(n)
	return err
}

func (gcm *generatorConfigMap) Update(current runtime.Object) error {
	tmpl, err := gcm.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gcm.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gcm.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gcm.GetTemplateName())
			return nil
		}
	}

	updated, err := gcm.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*corev1.ConfigMap)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gcm.client.ConfigMaps(gcm.Namespace).Update(n)
	return err
}

func (gcm *generatorConfigMap) Delete(opts *metav1.DeleteOptions) error {
	return gcm.client.ConfigMaps(gcm.Namespace).Delete(gcm.Name, opts)
}
