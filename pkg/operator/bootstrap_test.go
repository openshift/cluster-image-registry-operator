package operator

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configfakeclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageregistryfakeclient "github.com/openshift/client-go/imageregistry/clientset/versioned/fake"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func TestBootstrapAzure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configObjects := []runtime.Object{
		&configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.AzurePlatformType,
				},
			},
		},
	}

	registryObjects := []runtime.Object{
		&imageregistryv1.Config{
			TypeMeta: metav1.TypeMeta{
				APIVersion: imageregistryv1.GroupVersion.String(),
				Kind:       "Config",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        "cluster",
				Annotations: map[string]string{"installer-bootstrapped": ""},
			},
			Spec: imageregistryv1.ImageRegistrySpec{
				Storage: imageregistryv1.ImageRegistryConfigStorage{
					Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
						AccountName: "foo-bar",
						// NetworkAccess: &imageregistryv1.AzureNetworkAccess{
						// 	Type: imageregistryv1.AzureNetworkAccessTypeInternal,
						// },
					},
				},
			},
		},
	}

	configClient := configfakeclient.NewSimpleClientset(configObjects...)
	configInformerFactory := configinformers.NewSharedInformerFactory(configClient, 0)

	imageregistryClient := imageregistryfakeclient.NewSimpleClientset(registryObjects...)
	imageregistryInformerFactory := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)

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

	expected := imageregistryv1.ImageRegistrySpec{
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "foo-bar",
				// NetworkAccess: &imageregistryv1.AzureNetworkAccess{
				// 	Type: imageregistryv1.AzureNetworkAccessTypeInternal,
				// },
			},
		},
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState:  "Managed",
			LogLevel:         operatorv1.Normal,
			OperatorLogLevel: operatorv1.Normal,
		},
		Replicas:        2,
		RolloutStrategy: "RollingUpdate",
	}
	if !reflect.DeepEqual(config.Spec, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, config.Spec))
	}
}

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
	}

	configClient := configfakeclient.NewSimpleClientset(configObjects...)
	configInformerFactory := configinformers.NewSharedInformerFactory(configClient, 0)

	imageregistryClient := imageregistryfakeclient.NewSimpleClientset()
	imageregistryInformerFactory := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)

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

	expected := imageregistryv1.ImageRegistrySpec{
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			S3: &imageregistryv1.ImageRegistryConfigStorageS3{},
		},
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState:  "Managed",
			LogLevel:         operatorv1.Normal,
			OperatorLogLevel: operatorv1.Normal,
		},
		Replicas:        2,
		RolloutStrategy: "RollingUpdate",
	}
	if !reflect.DeepEqual(config.Spec, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, config.Spec))
	}
}
