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

var _ Templator = &generatorClusterRoleBinding{}

type generatorClusterRoleBinding struct {
	BaseTemplator
	client *rbacset.RbacV1Client
}

func makeClusterRoleBinding(g *Generator, cr *v1alpha1.ImageRegistry) (Templator, error) {
	client, err := rbacset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorClusterRoleBinding{
		BaseTemplator: BaseTemplator{
			Name:      "registry-registry-role",
			Strategy:  strategy.Override{},
			Generator: g,
		},
		client: client,
	}, nil
}

func (gcrb *generatorClusterRoleBinding) Expected() (runtime.Object, error) {
	crb := &rbacapi.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacapi.SchemeGroupVersion.String(),
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gcrb.Name,
			Namespace:   gcrb.Namespace,
			Annotations: gcrb.Annotations,
		},
		Subjects: []rbacapi.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      gcrb.Generator.params.Pod.ServiceAccount,
				Namespace: gcrb.Generator.params.Deployment.Namespace,
			},
		},
		RoleRef: rbacapi.RoleRef{
			Kind: "ClusterRole",
			Name: "system:registry",
		},
	}

	addOwnerRefToObject(crb, asOwner(gcrb.Generator.ImageRegistry))

	return crb, nil
}

func (gcrb *generatorClusterRoleBinding) Get() (runtime.Object, error) {
	return gcrb.client.ClusterRoleBindings().Get(gcrb.Name, metav1.GetOptions{})
}

func (gcrb *generatorClusterRoleBinding) Create() error {
	tmpl, err := gcrb.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*rbacapi.ClusterRoleBinding)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gcrb.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gcrb.client.ClusterRoleBindings().Create(n)
	return err
}

func (gcrb *generatorClusterRoleBinding) Update(current runtime.Object) error {
	tmpl, err := gcrb.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gcrb.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gcrb.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gcrb.GetTemplateName())
			return nil
		}
	}

	updated, err := gcrb.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*rbacapi.ClusterRoleBinding)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gcrb.client.ClusterRoleBindings().Update(n)
	return err
}

func (gcrb *generatorClusterRoleBinding) Delete(opts *metav1.DeleteOptions) error {
	return gcrb.client.ClusterRoleBindings().Delete(gcrb.Name, opts)
}
