package resource

import (
	"os"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	kcorelisters "k8s.io/client-go/listers/core/v1"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

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
        kubernetes.io/os: linux
      priorityClassName: system-cluster-critical
      tolerations:
      - effect: NoSchedule
        operator: Exists
      serviceAccountName: node-ca
      containers:
      - name: node-ca
        securityContext:
          privileged: true
        image: docker.io/openshift/origin-cluster-image-registry-operator:latest
        resources:
          requests:
            cpu: 10m
            memory: 10Mi
        command:
        - "/bin/sh"
        - "-c"
        - |
          trap 'jobs -p | xargs -r kill; echo shutting down node-ca; exit 0' TERM
          while [ true ];
          do
            for f in $(ls /tmp/serviceca); do
                echo $f
                ca_file_path="/tmp/serviceca/${f}"
                f=$(echo $f | sed  -r 's/(.*)\.\./\1:/')
                reg_dir_path="/etc/docker/certs.d/${f}"
                if [ -e "${reg_dir_path}" ]; then
                    cp -u $ca_file_path $reg_dir_path/ca.crt
                else
                    mkdir $reg_dir_path
                    cp $ca_file_path $reg_dir_path/ca.crt
                fi
            done
            for d in $(ls /etc/docker/certs.d); do
                echo $d
                dp=$(echo $d | sed  -r 's/(.*):/\1\.\./')
                reg_conf_path="/tmp/serviceca/${dp}"
                if [ ! -e "${reg_conf_path}" ]; then
                    rm -rf /etc/docker/certs.d/$d
                fi
            done
            sleep 60 & wait ${!}
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
	daemonSetLister appslisters.DaemonSetNamespaceLister
	serviceLister   kcorelisters.ServiceNamespaceLister
	client          appsclientv1.AppsV1Interface
	params          *parameters.Globals
}

func newGeneratorNodeCADaemonSet(daemonSetLister appslisters.DaemonSetNamespaceLister, serviceLister kcorelisters.ServiceNamespaceLister, client appsclientv1.AppsV1Interface, params *parameters.Globals) *generatorNodeCADaemonSet {
	return &generatorNodeCADaemonSet{
		daemonSetLister: daemonSetLister,
		serviceLister:   serviceLister,
		client:          client,
		params:          params,
	}
}

func (ds *generatorNodeCADaemonSet) Type() runtime.Object {
	return &appsv1.DaemonSet{}
}

func (ds *generatorNodeCADaemonSet) GetGroup() string {
	return appsv1.GroupName
}

func (ds *generatorNodeCADaemonSet) GetResource() string {
	return "daemonsets"
}

func (ds *generatorNodeCADaemonSet) GetNamespace() string {
	return ds.params.Deployment.Namespace
}

func (ds *generatorNodeCADaemonSet) GetName() string {
	return "node-ca"
}

func (ds *generatorNodeCADaemonSet) Get() (runtime.Object, error) {
	return ds.daemonSetLister.Get(ds.GetName())
}

func (ds *generatorNodeCADaemonSet) Create() (runtime.Object, error) {
	daemonSet := resourceread.ReadDaemonSetV1OrDie([]byte(nodeCADaemonSetDefinition))
	daemonSet.Spec.Template.Spec.Containers[0].Image = os.Getenv("IMAGE")

	return ds.client.DaemonSets(ds.GetNamespace()).Create(daemonSet)
}

func (ds *generatorNodeCADaemonSet) Update(o runtime.Object) (runtime.Object, bool, error) {
	daemonSet := o.(*appsv1.DaemonSet)
	modified := false

	newImage := os.Getenv("IMAGE")
	oldImage := daemonSet.Spec.Template.Spec.Containers[0].Image
	if newImage != oldImage {
		daemonSet.Spec.Template.Spec.Containers[0].Image = newImage
		modified = true
	}

	if !modified {
		return o, false, nil
	}

	n, err := ds.client.DaemonSets(ds.GetNamespace()).Update(daemonSet)
	return n, err == nil, err
}

func (ds *generatorNodeCADaemonSet) Delete(opts *metav1.DeleteOptions) error {
	return ds.client.DaemonSets(ds.GetNamespace()).Delete(ds.GetName(), opts)
}

func (ds *generatorNodeCADaemonSet) Owned() bool {
	// the nodeca daemon's lifecycle is not tied to the lifecycle of the registry
	return false
}
