package clusterroles

import (
	"fmt"

	"github.com/golang/glog"

	rbacapi "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubeset "k8s.io/client-go/kubernetes"
	rbaclisters "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	opcontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller"
)

var _ opcontroller.Watcher = &Controller{}

type Controller struct {
	lister rbaclisters.ClusterRoleLister
	synced cache.InformerSynced
}

func (c *Controller) Start(handler opcontroller.Handler, namespace string, stopCh <-chan struct{}) error {
	glog.Info("Starting clusterroles controller")

	kubeconfig, err := client.GetConfig()
	if err != nil {
		return err
	}

	kubeclient, err := kubeset.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclient, opcontroller.DefaultResyncDuration)
	informer := kubeInformerFactory.Rbac().V1().ClusterRoles()

	c.lister = informer.Lister()
	c.synced = informer.Informer().HasSynced

	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			handler("add", o)
		},
		UpdateFunc: func(o, n interface{}) {
			newObj := n.(*rbacapi.ClusterRole)
			oldObj := o.(*rbacapi.ClusterRole)
			if newObj.ResourceVersion == oldObj.ResourceVersion {
				return
			}
			handler("update", n)
		},
		DeleteFunc: func(o interface{}) {
			handler("delete", o)
		},
	})

	kubeInformerFactory.Start(stopCh)

	glog.Info("Waiting for clusterroles informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	return nil
}

func (c *Controller) Get(name, namespace string) (runtime.Object, error) {
	return c.lister.Get(name)
}
