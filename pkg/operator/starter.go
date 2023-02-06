package operator

import (
	"context"

	kubeinformers "k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageclient "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	imageregistryclient "github.com/openshift/client-go/imageregistry/clientset/versioned"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	routeclient "github.com/openshift/client-go/route/clientset/versioned"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/loglevel"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func RunOperator(ctx context.Context, kubeconfig *restclient.Config) error {
	kubeClient, err := kubeclient.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}
	configClient, err := configclient.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}
	imageregistryClient, err := imageregistryclient.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}
	routeClient, err := routeclient.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}
	imageClient, err := imageclient.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, defaultResyncDuration, kubeinformers.WithNamespace(defaults.ImageRegistryOperatorNamespace))
	kubeInformersForOpenShiftConfig := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, defaultResyncDuration, kubeinformers.WithNamespace(defaults.OpenShiftConfigNamespace))
	kubeInformersForOpenShiftConfigManaged := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, defaultResyncDuration, kubeinformers.WithNamespace(defaults.OpenShiftConfigManagedNamespace))
	kubeInformersForKubeSystem := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, defaultResyncDuration, kubeinformers.WithNamespace(kubeSystemNamespace))
	configInformers := configinformers.NewSharedInformerFactory(configClient, defaultResyncDuration)
	imageregistryInformers := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, defaultResyncDuration)
	routeInformers := routeinformers.NewSharedInformerFactoryWithOptions(routeClient, defaultResyncDuration, routeinformers.WithNamespace(defaults.ImageRegistryOperatorNamespace))
	imageInformers := imageinformers.NewSharedInformerFactory(imageClient, defaultResyncDuration)

	configOperatorClient := client.NewConfigOperatorClient(
		imageregistryClient.ImageregistryV1().Configs(),
		imageregistryInformers.Imageregistry().V1().Configs(),
	)

	// library-go just logs a warning and continues
	// https://github.com/openshift/library-go/blob/4362aa519714a4b62b00ab8318197ba2bba51cb7/pkg/controller/controllercmd/builder.go#L230
	controllerRef, err := events.GetControllerReferenceForCurrentPod(ctx, kubeClient, defaults.ImageRegistryOperatorNamespace, nil)
	if err != nil {
		klog.Warningf("unable to get owner reference (falling back to namespace): %v", err)
	}
	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events(defaults.ImageRegistryOperatorNamespace), "image-registry-operator", controllerRef)

	controller, err := NewController(
		eventRecorder,
		kubeconfig,
		kubeClient,
		configClient,
		imageregistryClient,
		routeClient,
		kubeInformers,
		kubeInformersForOpenShiftConfig,
		kubeInformersForOpenShiftConfigManaged,
		kubeInformersForKubeSystem,
		configInformers,
		imageregistryInformers,
		routeInformers,
	)
	if err != nil {
		return err
	}

	imageConfigStatusController, err := NewImageConfigController(
		configClient.ConfigV1(),
		configOperatorClient,
		routeInformers.Route().V1().Routes(),
		kubeInformers.Core().V1().Services(),
	)
	if err != nil {
		return err
	}

	clusterOperatorStatusController, err := NewClusterOperatorStatusController(
		[]configv1.ObjectReference{
			{Group: "imageregistry.operator.openshift.io", Resource: "configs", Name: "cluster"},
			{Group: "imageregistry.operator.openshift.io", Resource: "imagepruners", Name: "cluster"},
			{Group: "rbac.authorization.k8s.io", Resource: "clusterroles", Name: "system:registry"},
			{Group: "rbac.authorization.k8s.io", Resource: "clusterrolebindings", Name: "registry-registry-role"},
			{Group: "rbac.authorization.k8s.io", Resource: "clusterrolebindings", Name: "openshift-image-registry-pruner"},
			{Resource: "namespaces", Name: defaults.ImageRegistryOperatorNamespace},
		},
		configClient.ConfigV1(),
		configInformers.Config().V1().ClusterOperators(),
		imageregistryInformers.Imageregistry().V1().Configs(),
		imageregistryInformers.Imageregistry().V1().ImagePruners(),
		kubeInformers.Apps().V1().Deployments(),
	)
	if err != nil {
		return err
	}

	imageRegistryCertificatesController, err := NewImageRegistryCertificatesController(
		kubeconfig,
		kubeClient.CoreV1(),
		configOperatorClient,
		kubeInformers.Core().V1().ConfigMaps(),
		kubeInformers.Core().V1().Secrets(),
		kubeInformers.Core().V1().Services(),
		configInformers.Config().V1().Images(),
		configInformers.Config().V1().Infrastructures(),
		kubeInformersForOpenShiftConfig.Core().V1().ConfigMaps(),
		kubeInformersForOpenShiftConfigManaged.Core().V1().ConfigMaps(),
		imageregistryInformers.Imageregistry().V1().Configs(),
	)
	if err != nil {
		return err
	}

	nodeCADaemonController, err := NewNodeCADaemonController(
		eventRecorder,
		kubeClient.AppsV1(),
		configOperatorClient,
		kubeInformers.Apps().V1().DaemonSets(),
		kubeInformers.Core().V1().Services(),
	)
	if err != nil {
		return err
	}

	imagePrunerController, err := NewImagePrunerController(
		kubeClient,
		imageregistryClient,
		kubeInformers,
		imageregistryInformers,
		configInformers.Config().V1().Images(),
	)
	if err != nil {
		return err
	}

	loggingController := loglevel.NewClusterOperatorLoggingController(
		configOperatorClient,
		eventRecorder,
	)

	azureStackCloudController, err := NewAzureStackCloudController(
		configOperatorClient,
		kubeInformersForOpenShiftConfig.Core().V1().ConfigMaps(),
	)
	if err != nil {
		return err
	}

	metricsController := NewMetricsController(imageInformers.Image().V1().ImageStreams())

	kubeInformers.Start(ctx.Done())
	kubeInformersForOpenShiftConfig.Start(ctx.Done())
	kubeInformersForOpenShiftConfigManaged.Start(ctx.Done())
	kubeInformersForKubeSystem.Start(ctx.Done())
	configInformers.Start(ctx.Done())
	imageregistryInformers.Start(ctx.Done())
	routeInformers.Start(ctx.Done())
	imageInformers.Start(ctx.Done())

	go controller.Run(ctx.Done())
	go clusterOperatorStatusController.Run(ctx.Done())
	go nodeCADaemonController.Run(ctx.Done())
	go imageRegistryCertificatesController.Run(ctx.Done())
	go imageConfigStatusController.Run(ctx.Done())
	go imagePrunerController.Run(ctx.Done())
	go loggingController.Run(ctx, 1)
	go azureStackCloudController.Run(ctx)
	go metricsController.Run(ctx)

	<-ctx.Done()
	return nil
}
