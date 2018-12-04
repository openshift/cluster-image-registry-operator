package resource

import (
	"fmt"

	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/golang/glog"
	configapiv1 "github.com/openshift/api/config/v1"
	configsetv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Templator = &generatorImageConfig{}

type generatorImageConfig struct {
	BaseTemplator
	client *configsetv1.ConfigV1Client
}

func makeImageConfig(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := configsetv1.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorImageConfig{
		BaseTemplator: BaseTemplator{
			Name:      g.params.ImageConfig.Name,
			Strategy:  strategy.ImageConfig{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gic *generatorImageConfig) Expected() (runtime.Object, error) {
	ic := &configapiv1.Image{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configapiv1.SchemeGroupVersion.String(),
			Kind:       "Image",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gic.Name,
			Annotations: gic.Annotations,
		},
		Status: configapiv1.ImageStatus{
			InternalRegistryHostname: gic.Generator.ImageRegistry.Status.InternalRegistryHostname,
		},
	}

	addOwnerRefToObject(ic, asOwner(gic.Generator.ImageRegistry))

	return ic, nil
}

func (gic *generatorImageConfig) Get() (runtime.Object, error) {
	return gic.client.Images().Get(gic.Name, metav1.GetOptions{})
}

func (gic *generatorImageConfig) Create() error {
	tmpl, err := gic.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*configapiv1.Image)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gic.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gic.client.Images().Create(n)
	return err
}

func (gic *generatorImageConfig) Update(current runtime.Object) error {
	tmpl, err := gic.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gic.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gic.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gic.GetTemplateName())
			return nil
		}
	}

	updated, err := gic.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*configapiv1.Image)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gic.client.Images().Update(n)
	return err
}

func (gic *generatorImageConfig) Delete(opts *metav1.DeleteOptions) error {
	return gic.client.Images().Delete(gic.Name, opts)
}
