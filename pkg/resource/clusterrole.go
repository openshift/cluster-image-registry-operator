package resource

import (
	"fmt"

	rbacapi "k8s.io/api/rbac/v1"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"

	"github.com/golang/glog"
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Templator = &generatorClusterRole{}

type generatorClusterRole struct {
	BaseTemplator
	client *rbacset.RbacV1Client
}

func makeClusterRole(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := rbacset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorClusterRole{
		BaseTemplator: BaseTemplator{
			Name:      "system:registry",
			Strategy:  strategy.Override{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gcr *generatorClusterRole) Expected() (runtime.Object, error) {
	role := &rbacapi.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacapi.SchemeGroupVersion.String(),
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gcr.Name,
			Namespace:   gcr.Namespace,
			Annotations: gcr.Annotations,
		},
		Rules: []rbacapi.PolicyRule{
			{
				Verbs:     []string{"list"},
				APIGroups: []string{""},
				Resources: []string{
					"limitranges",
					"resourcequotas",
				},
			},
			{
				Verbs:     []string{"get"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreamimages",
					/* "imagestreams/layers", */
					"imagestreams/secrets",
				},
			},
			{
				Verbs:     []string{ /* "list", */ "get", "update"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreams",
				},
			},
			{
				Verbs:     []string{ /* "get", */ "delete"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreamtags",
				},
			},
			{
				Verbs:     []string{"get", "update" /*, "delete" */},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"images",
				},
			},
			{
				Verbs:     []string{"create"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreammappings",
				},
			},
		},
	}

	addOwnerRefToObject(role, asOwner(gcr.Generator.ImageRegistry))

	return role, nil
}

func (gcr *generatorClusterRole) Get() (runtime.Object, error) {
	return gcr.client.ClusterRoles().Get(gcr.Name, metav1.GetOptions{})
}

func (gcr *generatorClusterRole) Create() error {
	tmpl, err := gcr.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*rbacapi.ClusterRole)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gcr.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gcr.client.ClusterRoles().Create(n)
	return err
}

func (gcr *generatorClusterRole) Update(current runtime.Object) error {
	tmpl, err := gcr.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gcr.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gcr.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gcr.GetTemplateName())
			return nil
		}
	}

	updated, err := gcr.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*rbacapi.ClusterRole)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gcr.client.ClusterRoles().Update(n)
	return err
}

func (gcr *generatorClusterRole) Delete(opts *metav1.DeleteOptions) error {
	return gcr.client.ClusterRoles().Delete(gcr.Name, opts)
}
