package resource

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"

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

type templateGenerator func(*regopapi.ImageRegistry) (Template, error)

func NewGenerator(kubeconfig *rest.Config, params *parameters.Globals) *Generator {
	return &Generator{
		kubeconfig: kubeconfig,
		params:     params,
	}
}

type Generator struct {
	kubeconfig *rest.Config
	params     *parameters.Globals
}

func (g *Generator) makeTemplates(cr *regopapi.ImageRegistry) (ret []Template, err error) {
	generators := []templateGenerator{
		g.makeClusterRole,
		g.makeClusterRoleBinding,
		g.makeServiceAccount,
		g.makeConfigMap,
		g.makeSecret,
		g.makeService,
		g.makeImageConfig,
	}

	routes := g.getRouteGenerators(cr)
	for i := range routes {
		generators = append(generators, routes[i])
	}

	ret = make([]Template, len(generators)+1)
	resourceData := make([]string, len(generators))

	for i, gen := range generators {
		ret[i], err = gen(cr)
		if err != nil {
			return
		}

		resourceData[i], err = Checksum(ret[i].Expected())
		if err != nil {
			err = fmt.Errorf("unable to generate checksum for %s: %s", ret[i].Name(), err)
			return
		}
	}

	deploymentTmpl, err := g.makeDeployment(cr)
	if err != nil {
		return
	}

	dgst, err := Checksum(resourceData)
	if err != nil {
		err = fmt.Errorf("unable to generate checksum for %s dependencies: %s", deploymentTmpl.Name(), err)
		return
	}

	if deploymentTmpl.Annotations == nil {
		deploymentTmpl.Annotations = make(map[string]string)
	}

	deploymentTmpl.Annotations[parameters.ChecksumOperatorDepsAnnotation] = dgst

	ret[len(generators)] = deploymentTmpl

	return
}

func (g *Generator) syncRoutes(cr *regopapi.ImageRegistry, modified *bool) error {
	client, err := routeset.NewForConfig(g.kubeconfig)
	if err != nil {
		return err
	}

	routeList, err := client.Routes(g.params.Deployment.Namespace).List(metaapi.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list routes: %s", err)
	}

	routes := g.getRouteGenerators(cr)

	for name, gen := range routes {
		tmpl, err := gen(cr)
		if err != nil {
			return fmt.Errorf("unable to make template for route %s: %s", name, err)
		}

		err = g.applyTemplate(tmpl, modified)
		if err != nil {
			return fmt.Errorf("unable to apply template %s: %s", tmpl.Name(), err)
		}
	}

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

func (g *Generator) applyTemplate(tmpl Template, modified *bool) error {
	dgst, err := Checksum(tmpl.Expected())
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", tmpl.Name(), err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current, err := tmpl.Get()
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get object %s: %s", tmpl.Name(), err)
			}

			klog.Infof("creating object: %s", tmpl.Name())

			err = tmpl.Create()
			if err == nil {
				*modified = true
				return nil
			}
			return fmt.Errorf("failed to create object %s: %s", tmpl.Name(), err)
		}

		currentMeta, err := kmeta.Accessor(current)
		if err != nil {
			return fmt.Errorf("unable to get meta accessor for current object %s: %s", tmpl.Name(), err)
		}

		curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
		if ok && dgst == curdgst {
			klog.V(1).Infof("object has not changed: %s", tmpl.Name())
			return nil
		}

		updated, err := tmpl.Apply(current)
		if err != nil {
			return fmt.Errorf("unable to apply template %s: %s", tmpl.Name(), err)
		}

		updatedMeta, err := kmeta.Accessor(updated)
		if err != nil {
			return fmt.Errorf("unable to get meta accessor for updated object %s: %s", tmpl.Name(), err)
		}

		if updatedMeta.GetAnnotations() == nil {
			if tmpl.Annotations != nil {
				updatedMeta.SetAnnotations(tmpl.Annotations)
			} else {
				updatedMeta.SetAnnotations(map[string]string{})
			}
		}
		updatedMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation] = dgst

		klog.Infof("updating object: %s", tmpl.Name())

		err = tmpl.Update(updated)
		if err == nil {
			*modified = true
			return nil
		}
		return fmt.Errorf("failed to update object %s: %s", tmpl.Name(), err)
	})
}

func (g *Generator) Apply(cr *regopapi.ImageRegistry, modified *bool) error {
	templates, err := g.makeTemplates(cr)
	if err != nil {
		return fmt.Errorf("unable to generate templates: %s", err)
	}

	for _, tpl := range templates {
		err = g.applyTemplate(tpl, modified)
		if err != nil {
			return fmt.Errorf("unable to apply objects: %s", err)
		}
	}

	err = g.syncRoutes(cr, modified)
	if err != nil {
		return fmt.Errorf("unable to sync routes: %s", err)
	}

	return nil
}

func (g *Generator) removeByTemplate(tmpl Template, modified *bool) error {
	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground

	opts := &metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}

	klog.Infof("deleting object %s", tmpl.Name())

	err := tmpl.Delete(opts)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %s: %s", tmpl.Name(), err)
		}
		return nil
	}
	*modified = true
	return nil
}

func (g *Generator) Remove(cr *regopapi.ImageRegistry, modified *bool) error {
	templetes, err := g.makeTemplates(cr)
	if err != nil {
		return fmt.Errorf("unable to generate templates: %s", err)
	}

	for _, tmpl := range templetes {
		err = g.removeByTemplate(tmpl, modified)
		if err != nil {
			return fmt.Errorf("unable to remove objects: %s", err)
		}
		klog.Infof("resource %s removed", tmpl.Name())
	}

	return nil
}
