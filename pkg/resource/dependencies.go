package resource

import (
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

type dependencies struct {
	configMaps map[string]struct{}
	secrets    map[string]struct{}
}

func newDependencies() *dependencies {
	return &dependencies{
		configMaps: make(map[string]struct{}),
		secrets:    make(map[string]struct{}),
	}
}

func (d *dependencies) AddConfigMap(name string) {
	d.configMaps[name] = struct{}{}
}

func (d *dependencies) AddSecret(name string) {
	d.secrets[name] = struct{}{}
}

func (d dependencies) Checksum(configMapLister corelisters.ConfigMapNamespaceLister, secretLister corelisters.SecretNamespaceLister) (string, error) {
	var names []string
	var checksums []string

	for name := range d.configMaps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cm, err := configMapLister.Get(name)
		if errors.IsNotFound(err) {
			// We may have optional dependencies.
			continue
		} else if err != nil {
			return "", err
		}
		cm = cm.DeepCopy()
		cm.TypeMeta = metav1.TypeMeta{}
		cm.ObjectMeta = metav1.ObjectMeta{}
		dgst, err := strategy.Checksum(cm)
		if err != nil {
			return "", err
		}
		checksums = append(checksums, "configmap:"+name+":"+dgst)
	}

	for name := range d.secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sec, err := secretLister.Get(name)
		if errors.IsNotFound(err) {
			// We may have optional dependencies.
			continue
		} else if err != nil {
			return "", err
		}
		sec = sec.DeepCopy()
		sec.TypeMeta = metav1.TypeMeta{}
		sec.ObjectMeta = metav1.ObjectMeta{}
		dgst, err := strategy.Checksum(sec)
		if err != nil {
			return "", err
		}
		checksums = append(checksums, "secret:"+name+":"+dgst)
	}

	return strategy.Checksum(checksums)
}
