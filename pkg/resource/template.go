package resource

import (
	"fmt"

	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

type Templator interface {
	GetName() string
	SetName(string)
	GetNamespace() string
	SetNamespace(string)
	GetAnnotations() map[string]string
	SetAnnotations(annotations map[string]string)
	GetStrategy() strategy.Strategy
	SetStrategy(strategy.Strategy)
	GetTemplateName() string
	Expected() (runtime.Object, error)
	Get() (runtime.Object, error)
	Create() error
	Update(runtime.Object) error
	Delete(*metaapi.DeleteOptions) error
}

type BaseTemplator struct {
	Name        string
	Namespace   string
	Annotations map[string]string
	Strategy    strategy.Strategy
	Generator   *Generator
}

func (base *BaseTemplator) GetName() string {
	return base.Name
}

func (base *BaseTemplator) SetName(s string) {
	base.Name = s
}

func (base *BaseTemplator) GetNamespace() string {
	return base.Namespace
}

func (base *BaseTemplator) SetNamespace(s string) {
	base.Namespace = s
}

func (base *BaseTemplator) GetAnnotations() map[string]string {
	return base.Annotations
}

func (base *BaseTemplator) SetAnnotations(annotations map[string]string) {
	base.Annotations = annotations
}

func (base *BaseTemplator) GetStrategy() strategy.Strategy {
	return base.Strategy
}

func (base *BaseTemplator) SetStrategy(s strategy.Strategy) {
	base.Strategy = s
}

func (base *BaseTemplator) GetTemplateName() string {
	var name string

	if base.Namespace != "" {
		name = fmt.Sprintf("Namespace=%s, ", base.Namespace)
	}

	name += fmt.Sprintf("Name=%s", base.Name)

	return name
}

func (base *BaseTemplator) UpdateChecksumAnnotation(tmpl runtime.Object, m metaapi.Object) error {
	dgst, err := Checksum(tmpl)
	if err != nil {
		return err
	}

	annotations := m.GetAnnotations()

	if annotations != nil {
		annotations = map[string]string{}
	}

	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	m.SetAnnotations(annotations)

	return nil
}
