package routes

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	routeapi "github.com/openshift/api/route/v1"
	routeset "github.com/openshift/client-go/route/clientset/versioned"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	opcontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller"
)

var _ opcontroller.Watcher = &Controller{}

type Controller struct {
	lister routelisters.RouteLister
	synced cache.InformerSynced
}

func (c *Controller) Start(handler opcontroller.Handler, namespace string, stopCh <-chan struct{}) error {
	glog.Info("Starting routes controller")

	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		return err
	}

	client, err := routeset.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	informerFactory := routeinformers.NewSharedInformerFactoryWithOptions(client, opcontroller.DefaultResyncDuration, routeinformers.WithNamespace(namespace))
	informer := informerFactory.Route().V1().Routes()

	c.lister = informer.Lister()
	c.synced = informer.Informer().HasSynced

	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			handler("add", o)
		},
		UpdateFunc: func(o, n interface{}) {
			newObj := n.(*routeapi.Route)
			oldObj := o.(*routeapi.Route)
			if newObj.ResourceVersion == oldObj.ResourceVersion {
				return
			}
			handler("update", n)
		},
		DeleteFunc: func(o interface{}) {
			handler("delete", o)
		},
	})

	informerFactory.Start(stopCh)

	glog.Info("Waiting for routes informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	return nil
}

func (c *Controller) Get(name, namespace string) (runtime.Object, error) {
	return c.lister.Routes(namespace).Get(name)
}
