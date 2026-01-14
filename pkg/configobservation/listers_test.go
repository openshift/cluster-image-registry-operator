package configobservation

import (
	"testing"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	configfake "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageregistryfake "github.com/openshift/client-go/imageregistry/clientset/versioned/fake"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func TestAPIServerLister(t *testing.T) {
	configClient := configfake.NewSimpleClientset()
	configInformers := configinformers.NewSharedInformerFactory(configClient, 0)

	imageregistryClient := imageregistryfake.NewSimpleClientset()
	imageregistryInformers := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)

	operatorClient := client.NewConfigOperatorClient(
		imageregistryClient.ImageregistryV1().Configs(),
		imageregistryInformers.Imageregistry().V1().Configs(),
	)

	apiServerInformer := configInformers.Config().V1().APIServers()

	listers := NewAPIServerConfigListers(apiServerInformer, operatorClient)
	impl := listers.(*apiServerConfigListers)

	lister := impl.APIServerLister()
	if lister == nil {
		t.Error("APIServerLister() returned nil")
	}

	if lister != impl.apiServerLister {
		t.Error("APIServerLister() did not return the expected lister")
	}
}

func TestPreRunHasSynced(t *testing.T) {
	configClient := configfake.NewSimpleClientset()
	configInformers := configinformers.NewSharedInformerFactory(configClient, 0)

	imageregistryClient := imageregistryfake.NewSimpleClientset()
	imageregistryInformers := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)

	operatorClient := client.NewConfigOperatorClient(
		imageregistryClient.ImageregistryV1().Configs(),
		imageregistryInformers.Imageregistry().V1().Configs(),
	)

	apiServerInformer := configInformers.Config().V1().APIServers()

	listers := NewAPIServerConfigListers(apiServerInformer, operatorClient)
	impl := listers.(*apiServerConfigListers)

	syncs := impl.PreRunHasSynced()
	if len(syncs) != 2 {
		t.Errorf("expected 2 informer syncs, got %d", len(syncs))
	}

	for i, sync := range syncs {
		if sync == nil {
			t.Errorf("informer sync %d is nil", i)
		}
	}
}

func TestResourceSyncerPanics(t *testing.T) {
	configClient := configfake.NewSimpleClientset()
	configInformers := configinformers.NewSharedInformerFactory(configClient, 0)

	imageregistryClient := imageregistryfake.NewSimpleClientset()
	imageregistryInformers := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)

	operatorClient := client.NewConfigOperatorClient(
		imageregistryClient.ImageregistryV1().Configs(),
		imageregistryInformers.Imageregistry().V1().Configs(),
	)

	apiServerInformer := configInformers.Config().V1().APIServers()

	listers := NewAPIServerConfigListers(apiServerInformer, operatorClient)
	impl := listers.(*apiServerConfigListers)

	defer func() {
		if r := recover(); r == nil {
			t.Error("ResourceSyncer() did not panic")
		}
	}()

	impl.ResourceSyncer()
}

func TestInformerSyncsAreCallable(t *testing.T) {
	configClient := configfake.NewSimpleClientset()
	configInformers := configinformers.NewSharedInformerFactory(configClient, 0)

	imageregistryClient := imageregistryfake.NewSimpleClientset(&imageregistryv1.Config{})
	imageregistryInformers := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)

	operatorClient := client.NewConfigOperatorClient(
		imageregistryClient.ImageregistryV1().Configs(),
		imageregistryInformers.Imageregistry().V1().Configs(),
	)

	apiServerInformer := configInformers.Config().V1().APIServers()

	listers := NewAPIServerConfigListers(apiServerInformer, operatorClient)
	impl := listers.(*apiServerConfigListers)

	syncs := impl.PreRunHasSynced()

	for i, sync := range syncs {
		if sync() {
			// before starting informers, they should return false
			t.Errorf("informer sync %d returned true before informer started", i)
		}
	}
}
