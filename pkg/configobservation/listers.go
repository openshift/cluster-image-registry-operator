package configobservation

import (
	"k8s.io/client-go/tools/cache"

	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/configobserver/apiserver"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

// apiServerConfigListers implements the configobserver.Listers interface for APIServer observation
type apiServerConfigListers struct {
	apiServerLister configlistersv1.APIServerLister
	informerSyncs   []cache.InformerSynced
}

// Ensure apiServerConfigListers implements the required interfaces
var (
	_ configobserver.Listers    = &apiServerConfigListers{}
	_ apiserver.APIServerLister = &apiServerConfigListers{}
)

// ResourceSyncer panics - we don't support resource syncing
func (l *apiServerConfigListers) ResourceSyncer() resourcesynccontroller.ResourceSyncer {
	panic("ResourceSyncer is not supported by apiServerConfigListers")
}

// PreRunHasSynced returns the informer sync functions
func (l *apiServerConfigListers) PreRunHasSynced() []cache.InformerSynced {
	return l.informerSyncs
}

// APIServerLister returns the APIServer lister
func (l *apiServerConfigListers) APIServerLister() configlistersv1.APIServerLister {
	return l.apiServerLister
}

// NewAPIServerConfigListers creates a new apiServerConfigListers instance
func NewAPIServerConfigListers(
	apiServerInformer configinformersv1.APIServerInformer,
	operatorClient v1helpers.OperatorClient,
) configobserver.Listers {
	return &apiServerConfigListers{
		apiServerLister: apiServerInformer.Lister(),
		informerSyncs: []cache.InformerSynced{
			apiServerInformer.Informer().HasSynced,
			operatorClient.Informer().HasSynced,
		},
	}
}
