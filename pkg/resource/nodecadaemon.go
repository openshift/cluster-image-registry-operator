package resource

import (
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

const (
	nodeCADaemonSetDefinition = `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-ca
  namespace: openshift-image-registry
spec:
  selector:
    matchLabels:
      name: node-ca
  template:
    metadata:
      labels:
        name: node-ca
    spec:      
      nodeSelector:
        beta.kubernetes.io/os: linux
      priorityClassName: system-cluster-critical
      tolerations:
      - effect: NoSchedule
        key: node-role.kubernetes.io/master
        operator: Exists
      serviceAccountName: node-ca
      containers:
      - name: node-ca
        securityContext:
          privileged: true
        image: docker.io/openshift/origin-cluster-image-registry-operator:latest
        command: 
        - "/bin/sh"
        - "-c"
        - |
          if [ ! -e /etc/docker/certs.d/image-registry.openshift-image-registry.svc.cluster.local:5000 ]; then
            mkdir /etc/docker/certs.d/image-registry.openshift-image-registry.svc.cluster.local:5000
          fi
          if [ ! -e /etc/docker/certs.d/image-registry.openshift-image-registry.svc:5000 ]; then
            mkdir /etc/docker/certs.d/image-registry.openshift-image-registry.svc:5000
          fi
          while [ true ];
          do
            if [ -e /tmp/serviceca/service-ca.crt ]; then
              cp -u /tmp/serviceca/service-ca.crt /etc/docker/certs.d/image-registry.openshift-image-registry.svc.cluster.local:5000
              cp -u /tmp/serviceca/service-ca.crt /etc/docker/certs.d/image-registry.openshift-image-registry.svc:5000
            else 
              rm /etc/docker/certs.d/image-registry.openshift-image-registry.svc.cluster.local:5000/service-ca.crt
              rm /etc/docker/certs.d/image-registry.openshift-image-registry.svc:5000/service-ca.crt
            fi
            sleep 60
          done
        volumeMounts:
        - name: serviceca
          mountPath: /tmp/serviceca
        - name: host
          mountPath: /etc/docker/certs.d
      volumes:
      - name: host
        hostPath:
          path: /etc/docker/certs.d
      - name: serviceca
        configMap:
          name: image-registry-certificates
`
)

var _ Mutator = &generatorNodeCADaemonSet{}

type generatorNodeCADaemonSet struct {
	lister   appslisters.DaemonSetNamespaceLister
	client   appsclientv1.AppsV1Interface
	hostname string
	owner    metav1.OwnerReference
	params   *parameters.Globals
}

func newGeneratorNodeCADaemonSet(lister appslisters.DaemonSetNamespaceLister, client appsclientv1.AppsV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry) *generatorNodeCADaemonSet {
	return &generatorNodeCADaemonSet{
		lister:   lister,
		client:   client,
		params:   params,
		hostname: cr.Status.InternalRegistryHostname,
	}
}

func (ds *generatorNodeCADaemonSet) Type() runtime.Object {
	return &appsv1.DaemonSet{}
}

func (ds *generatorNodeCADaemonSet) GetNamespace() string {
	return ds.params.Deployment.Namespace
}

func (ds *generatorNodeCADaemonSet) GetName() string {
	return "node-ca"
}

func (ds *generatorNodeCADaemonSet) Get() (runtime.Object, error) {
	return ds.lister.Get(ds.GetName())
}

func (ds *generatorNodeCADaemonSet) Create() error {
	daemonSet := resourceread.ReadDaemonSetV1OrDie([]byte(nodeCADaemonSetDefinition))
	env := corev1.EnvVar{
		Name:  "internalRegistryHostname",
		Value: ds.hostname,
	}
	daemonSet.Spec.Template.Spec.Containers[0].Image = os.Getenv("IMAGE")
	daemonSet.Spec.Template.Spec.Containers[0].Env = append(daemonSet.Spec.Template.Spec.Containers[0].Env, env)
	_, err := ds.client.DaemonSets(ds.GetNamespace()).Create(daemonSet)
	return err
}

func (ds *generatorNodeCADaemonSet) Update(o runtime.Object) (bool, error) {
	daemonSet := o.(*appsv1.DaemonSet)
	modified := false
	exists := false
	for i, env := range daemonSet.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "internalRegistryHostname" {
			exists = true
			if env.Value != ds.hostname {
				daemonSet.Spec.Template.Spec.Containers[0].Env[i].Value = ds.hostname
				modified = true
			}
			break
		}
	}
	if !exists {
		env := corev1.EnvVar{
			Name:  "internalRegistryHostname",
			Value: ds.hostname,
		}
		daemonSet.Spec.Template.Spec.Containers[0].Env = append(daemonSet.Spec.Template.Spec.Containers[0].Env, env)
		modified = true
	}

	if !modified {
		return false, nil
	}

	_, err := ds.client.DaemonSets(ds.GetNamespace()).Update(daemonSet)
	return true, err
}

func (ds *generatorNodeCADaemonSet) Delete(opts *metav1.DeleteOptions) error {
	return ds.client.DaemonSets(ds.GetNamespace()).Delete(ds.GetName(), opts)
}
