package resource

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/golang/glog"

	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func Checksum(o interface{}) (string, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

type ResourceGenerator func(*Generator, *regopapi.ImageRegistry) (Templator, error)

func NewGenerator(kubeconfig *rest.Config, params *parameters.Globals) *Generator {
	return &Generator{
		kubeconfig: kubeconfig,
		params:     params,
	}
}

type Generator struct {
	kubeconfig    *rest.Config
	params        *parameters.Globals
	ImageRegistry *regopapi.ImageRegistry
}

func (g *Generator) getSecret(name, namespace string) (*coreapi.Secret, error) {
	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}
	return client.Secrets(namespace).Get(name, metaapi.GetOptions{})
}

func (g *Generator) removeObsoleteRoutes(cr *regopapi.ImageRegistry, modified *bool) error {
	client, err := routeset.NewForConfig(g.kubeconfig)
	if err != nil {
		return err
	}

	routeList, err := client.Routes(g.params.Deployment.Namespace).List(metaapi.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list routes: %s", err)
	}

	routes := g.getRouteGenerators(cr)

	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground

	opts := &metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}

	for _, route := range routeList.Items {
		if !metaapi.IsControlledBy(&route, cr) {
			continue
		}
		if _, found := routes[route.ObjectMeta.Name]; found {
			continue
		}
		err = client.Routes(g.params.Deployment.Namespace).Delete(route.ObjectMeta.Name, opts)
		if err != nil {
			return err
		}
		*modified = true
	}

	return nil
}

func (g *Generator) List(cr *regopapi.ImageRegistry) []ResourceGenerator {
	generators := []ResourceGenerator{
		makeClusterRole,
		makeClusterRoleBinding,
		makeServiceAccount,
		makeConfigMap,
		makeSecret,
		makeService,
		makeImageConfig,
	}

	routes := g.getRouteGenerators(cr)
	for i := range routes {
		generators = append(generators, routes[i])
	}

	return append(generators, makeDeployment)
}

func (g *Generator) Apply(cr *regopapi.ImageRegistry, modified *bool) error {
	var resourceData []string

	generators := g.List(cr)

	for i, gen := range generators {
		template, err := gen(g, cr)
		if err != nil {
			return err
		}

		if i < len(generators)-1 {
			tmpl, err := template.Expected()
			if err != nil {
				return err
			}

			dgst, err := Checksum(tmpl)
			if err != nil {
				return fmt.Errorf("unable to generate checksum for %s: %s", template.GetTemplateName(), err)
			}
			resourceData = append(resourceData, dgst)

		} else {
			dgst, err := Checksum(resourceData)
			if err != nil {
				return fmt.Errorf("unable to generate checksum for %s dependencies: %s", template.GetTemplateName(), err)
			}

			annotations := template.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[parameters.ChecksumOperatorDepsAnnotation] = dgst
			template.SetAnnotations(annotations)
		}

		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current, err := template.Get()
			if err != nil {
				if !errors.IsNotFound(err) {
					return fmt.Errorf("failed to get object %s: %s", template.GetTemplateName(), err)
				}

				err = template.Create()
				if err == nil {
					glog.Infof("object %s created", template.GetTemplateName())
					*modified = true
					return nil
				}
				return fmt.Errorf("failed to create object %s: %s", template.GetTemplateName(), err)
			}

			err = template.Update(current)
			if err == nil {
				glog.Infof("object %s updated", template.GetTemplateName())
				*modified = true
				return nil
			}
			return fmt.Errorf("failed to update object %s: %s", template.GetTemplateName(), err)
		})
		if err != nil {
			return fmt.Errorf("unable to apply objects: %s", err)
		}
	}

	err := g.removeObsoleteRoutes(cr, modified)
	if err != nil {
		return fmt.Errorf("unable to remove obsolete routes: %s", err)
	}

	return nil
}

func (g *Generator) Remove(cr *regopapi.ImageRegistry, modified *bool) error {
	var templetes []Templator

	for _, gen := range g.List(cr) {
		template, err := gen(g, cr)
		if err != nil {
			return err
		}
		templetes = append(templetes, template)
	}

	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground

	opts := &metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}

	for _, tmpl := range templetes {
		err := tmpl.Delete(opts)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s: %s", tmpl.GetTemplateName(), err)
			}
		} else {
			glog.Infof("object %s deleted", tmpl.GetTemplateName())
			*modified = true
		}
	}

	return nil
}
