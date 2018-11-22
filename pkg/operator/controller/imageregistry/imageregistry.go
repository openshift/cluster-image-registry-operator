package imageregistry

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	imageregistryset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned"
	imageregistryinformers "github.com/openshift/cluster-image-registry-operator/pkg/generated/informers/externalversions"
	imageregistrylisters "github.com/openshift/cluster-image-registry-operator/pkg/generated/listers/imageregistry/v1alpha1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	opcontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller"
)

var _ opcontroller.Watcher = &Controller{}

type Controller struct {
	lister imageregistrylisters.ImageRegistryLister
	synced cache.InformerSynced
}

func (c *Controller) Start(handler opcontroller.Handler, namespace string, stopCh <-chan struct{}) error {
	klog.Info("Starting imageregistry controller")

	kubeconfig, err := client.GetConfig()
	if err != nil {
		return err
	}

	client, err := imageregistryset.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	informerFactory := imageregistryinformers.NewSharedInformerFactory(client, opcontroller.DefaultResyncDuration)
	informer := informerFactory.Imageregistry().V1alpha1().ImageRegistries()

	c.lister = informer.Lister()
	c.synced = informer.Informer().HasSynced

	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			handler("add", o)
		},
		UpdateFunc: func(o, n interface{}) {
			newObj := n.(*imageregistryapi.ImageRegistry)
			oldObj := o.(*imageregistryapi.ImageRegistry)
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

	klog.Info("Waiting for imageregistry informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	return nil
}

func (c *Controller) Get(name, namespace string) (runtime.Object, error) {
	return c.lister.Get(name)
}
