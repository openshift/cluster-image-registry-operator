package resource

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/golang/glog"
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Templator = &generatorService{}

type generatorService struct {
	BaseTemplator
	client *coreset.CoreV1Client
}

func makeService(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorService{
		BaseTemplator: BaseTemplator{
			Name:      g.params.Service.Name,
			Namespace: g.params.Deployment.Namespace,
			Strategy:  strategy.Service{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gs *generatorService) Expected() (runtime.Object, error) {
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gs.Name,
			Namespace:   gs.Namespace,
			Annotations: gs.Annotations,
			Labels:      gs.Generator.params.Deployment.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: gs.Generator.params.Deployment.Labels,
			Ports: []corev1.ServicePort{
				{
					Name:       fmt.Sprintf("%d-tcp", gs.Generator.params.Container.Port),
					Port:       int32(gs.Generator.params.Container.Port),
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(gs.Generator.params.Container.Port),
				},
			},
		},
	}

	if gs.Generator.ImageRegistry.Spec.TLS {
		svc.ObjectMeta.Annotations = map[string]string{
			"service.alpha.openshift.io/serving-cert-secret-name": gs.Generator.ImageRegistry.GetName() + "-tls",
		}
	}

	addOwnerRefToObject(svc, asOwner(gs.Generator.ImageRegistry))

	return svc, nil
}

func (gs *generatorService) Get() (runtime.Object, error) {
	return gs.client.Services(gs.Namespace).Get(gs.Name, metav1.GetOptions{})
}

func (gs *generatorService) Create() error {
	tmpl, err := gs.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*corev1.Service)

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

	_, err = gs.client.Services(gs.Namespace).Create(n)
	return err
}

func (gs *generatorService) Update(current runtime.Object) error {
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

	n := updated.(*corev1.Service)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gs.client.Services(gs.Namespace).Update(n)
	return err
}

func (gs *generatorService) Delete(opts *metav1.DeleteOptions) error {
	return gs.client.Services(gs.Namespace).Delete(gs.Name, opts)
}
