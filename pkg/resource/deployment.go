package resource

import (
	"fmt"

	kappsapi "k8s.io/api/apps/v1"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	kappsset "k8s.io/client-go/kubernetes/typed/apps/v1"

	"github.com/golang/glog"
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Templator = &generatorDeployment{}

type generatorDeployment struct {
	BaseTemplator
	client *kappsset.AppsV1Client
}

func makeDeployment(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := kappsset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorDeployment{
		BaseTemplator: BaseTemplator{
			Name:      cr.ObjectMeta.Name,
			Namespace: g.params.Deployment.Namespace,
			Strategy:  strategy.Deployment{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gd *generatorDeployment) Expected() (runtime.Object, error) {
	podTemplateSpec, annotations, err := gd.Generator.makePodTemplateSpec(gd.Generator.ImageRegistry)
	if err != nil {
		return nil, err
	}

	if annotations == nil {
		annotations = map[string]string{}
	}

	for k, v := range gd.Annotations {
		annotations[k] = v
	}

	dc := &kappsapi.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kappsapi.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gd.Name,
			Namespace:   gd.Namespace,
			Labels:      gd.Generator.params.Deployment.Labels,
			Annotations: annotations,
		},
		Spec: kappsapi.DeploymentSpec{
			Replicas: &gd.Generator.ImageRegistry.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: gd.Generator.params.Deployment.Labels,
			},
			Template: podTemplateSpec,
		},
	}

	addOwnerRefToObject(dc, asOwner(gd.Generator.ImageRegistry))

	return dc, nil
}

func (gd *generatorDeployment) Get() (runtime.Object, error) {
	return gd.client.Deployments(gd.Namespace).Get(gd.Name, metav1.GetOptions{})
}

func (gd *generatorDeployment) Create() error {
	tmpl, err := gd.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*kappsapi.Deployment)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gd.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gd.client.Deployments(gd.Namespace).Create(n)
	return err
}

func (gd *generatorDeployment) Update(current runtime.Object) error {
	tmpl, err := gd.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gd.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gd.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gd.GetTemplateName())
			return nil
		}
	}

	updated, err := gd.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*kappsapi.Deployment)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gd.client.Deployments(gd.Namespace).Update(n)
	return err
}

func (gd *generatorDeployment) Delete(opts *metav1.DeleteOptions) error {
	return gd.client.Deployments(gd.Namespace).Delete(gd.Name, opts)
}
