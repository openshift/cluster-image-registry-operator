package operator

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configfakeclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageregistryfakeclient "github.com/openshift/client-go/imageregistry/clientset/versioned/fake"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/events"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/configobservation"
)

func TestBootstrapAWS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configObjects := []runtime.Object{
		&configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.AWSPlatformType,
				},
			},
		},
		&configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
		},
	}

	configClient := configfakeclient.NewSimpleClientset(configObjects...)
	configInformerFactory := configinformers.NewSharedInformerFactory(configClient, 0)

	imageregistryClient := imageregistryfakeclient.NewSimpleClientset()
	imageregistryInformerFactory := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)

	operatorClient := client.NewConfigOperatorClient(
		imageregistryClient.ImageregistryV1().Configs(),
		imageregistryInformerFactory.Imageregistry().V1().Configs(),
	)

	apiLister := configobservation.NewAPIServerConfigListers(
		configInformerFactory.Config().V1().APIServers(),
		operatorClient,
	)

	c := &Controller{
		listers: &client.Listers{
			StorageListers: client.StorageListers{
				Infrastructures: configInformerFactory.Config().V1().Infrastructures().Lister(),
			},
			RegistryConfigs: imageregistryInformerFactory.Imageregistry().V1().Configs().Lister(),
		},
		clients: &client.Clients{
			RegOp: imageregistryClient,
		},
		apiLister:  apiLister,
		evRecorder: events.NewInMemoryRecorder("bootstrap-test", clock.RealClock{}),
	}

	configInformerFactory.Start(ctx.Done())
	imageregistryInformerFactory.Start(ctx.Done())
	configInformerFactory.WaitForCacheSync(ctx.Done())
	imageregistryInformerFactory.WaitForCacheSync(ctx.Done())

	if err := c.Bootstrap(); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	config, err := imageregistryClient.ImageregistryV1().Configs().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Verify ObservedConfig is populated
	if len(config.Spec.ObservedConfig.Raw) == 0 {
		t.Error("expected ObservedConfig to be populated")
	}

	// Compare the rest of the spec (excluding ObservedConfig)
	expectedSpec := imageregistryv1.ImageRegistrySpec{
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			S3: &imageregistryv1.ImageRegistryConfigStorageS3{},
		},
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState:  "Managed",
			LogLevel:         operatorv1.Normal,
			OperatorLogLevel: operatorv1.Normal,
			ObservedConfig:   config.Spec.ObservedConfig,
		},
		Replicas:        2,
		RolloutStrategy: "RollingUpdate",
	}
	if !reflect.DeepEqual(config.Spec, expectedSpec) {
		t.Errorf("unexpected config: %s", cmp.Diff(expectedSpec, config.Spec))
	}
}
