package operator

import (
	"context"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubefakeclient "k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/utils/clock"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configfakeclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageregistryfakeclient "github.com/openshift/client-go/imageregistry/clientset/versioned/fake"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	routefakeclient "github.com/openshift/client-go/route/clientset/versioned/fake"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/configobserver/apiserver"
	"github.com/openshift/library-go/pkg/operator/events"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	localconfigobservation "github.com/openshift/cluster-image-registry-operator/pkg/configobservation"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

// testControllerSetup holds the fake clients and controller for testing.
// the controller requires quite a lot of clients so we need a helper
// struct to keep track of them all. this struct also holds a reference
// to the controller itself.
type testControllerSetup struct {
	controller                   *Controller
	kubeClient                   *kubefakeclient.Clientset
	configClient                 *configfakeclient.Clientset
	regClient                    *imageregistryfakeclient.Clientset
	routeClient                  *routefakeclient.Clientset
	kubeInformerFactory          kubeinformers.SharedInformerFactory
	configInformerFactory        configinformers.SharedInformerFactory
	imageregistryInformerFactory imageregistryinformers.SharedInformerFactory
	routeInformerFactory         routeinformers.SharedInformerFactory
	operatorClient               *client.ConfigOperatorClient
	apiLister                    configobserver.Listers
}

// registryConfigResourceVersionBumper returns a reaction function that
// bumps the config resource version every time an update happens. the ir
// controller ignore events if the resource version isn't bumped and the
// fake client does not seem to keep track of this.
func registryConfigResourceVersionBumper(t *testing.T, regcli *imageregistryfakeclient.Clientset) clientgotesting.ReactionFunc {
	return func(action clientgotesting.Action) (bool, runtime.Object, error) {
		update := action.(clientgotesting.UpdateAction)
		newcfg := update.GetObject().(*imageregistryv1.Config).DeepCopy()

		gvr := schema.GroupVersionResource{
			Group:    "imageregistry.operator.openshift.io",
			Version:  "v1",
			Resource: "configs",
		}

		tmpobj, err := regcli.Tracker().Get(gvr, "", newcfg.Name)
		if tmpobj == nil {
			if err != nil {
				t.Fatalf("failed to get object from tracker: %v", err)
			}
			t.Fatal("update on a non existent object")
		}

		// bump the resource version.
		oldcfg := tmpobj.(*imageregistryv1.Config)
		if oldcfg.ResourceVersion == "" {
			oldcfg.ResourceVersion = "0"
		}

		rv, err := strconv.Atoi(oldcfg.ResourceVersion)
		if err != nil {
			t.Fatalf("failed to parse resource version as int: %v", err)
		}
		nextrv := strconv.Itoa(rv + 1)

		if update.GetSubresource() == "status" {
			newcfg.Spec = oldcfg.Spec
			newcfg.ResourceVersion = nextrv
			err := regcli.Tracker().Update(gvr, newcfg, "")
			return true, newcfg, err
		}

		newcfg.ResourceVersion = nextrv
		newcfg.Generation = oldcfg.Generation + 1
		err = regcli.Tracker().Update(gvr, newcfg, "")
		return true, newcfg, err
	}
}

// newTestControllerSetup creates fake clients and informer factories
// with the openshift-image-registry namespace. tests can add additional
// objects using setup.kubeClient.Tracker().Add(obj). after adding
// objects, call setup.start(t, ctx, startRunLoop) to create the controller
// and start informers.
func newTestControllerSetup(t *testing.T) *testControllerSetup {
	kubeClient := kubefakeclient.NewSimpleClientset()
	configClient := configfakeclient.NewSimpleClientset()
	routeClient := routefakeclient.NewSimpleClientset()

	// create the registry config fake client set and make sure we have
	// a reactor to bump the resource version upon updates.
	regClient := imageregistryfakeclient.NewSimpleClientset()
	regClient.PrependReactor("update", "configs", registryConfigResourceVersionBumper(t, regClient))

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	configInformerFactory := configinformers.NewSharedInformerFactory(configClient, 0)
	imageregistryInformerFactory := imageregistryinformers.NewSharedInformerFactory(regClient, 0)
	routeInformerFactory := routeinformers.NewSharedInformerFactory(routeClient, 0)

	operatorClient := client.NewConfigOperatorClient(
		regClient.ImageregistryV1().Configs(),
		imageregistryInformerFactory.Imageregistry().V1().Configs(),
	)

	apiLister := localconfigobservation.NewAPIServerConfigListers(
		configInformerFactory.Config().V1().APIServers(),
		operatorClient,
	)

	setup := &testControllerSetup{
		kubeClient:                   kubeClient,
		configClient:                 configClient,
		regClient:                    regClient,
		routeClient:                  routeClient,
		kubeInformerFactory:          kubeInformerFactory,
		configInformerFactory:        configInformerFactory,
		imageregistryInformerFactory: imageregistryInformerFactory,
		routeInformerFactory:         routeInformerFactory,
		operatorClient:               operatorClient,
		apiLister:                    apiLister,
	}

	// add the operator namespace with required scc annotations. without
	// this object the controller will certainly fail.
	if err := setup.kubeClient.Tracker().Add(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaults.ImageRegistryOperatorNamespace,
			Annotations: map[string]string{
				"openshift.io/sa.scc.supplemental-groups": "1000/10000",
			},
		},
	}); err != nil {
		t.Fatalf("failed to add namespace to tracker: %v", err)
	}

	return setup
}

// start creates the controller and starts all informers. If startRunLoop is
// true, it also launches the controller's Run() loop in a goroutine for
// background reconciliation. If false, only informers are started (useful for
// testing controller methods directly without background reconciliation
// racing).
func (s *testControllerSetup) start(t *testing.T, ctx context.Context, startRunLoop bool) {
	openshiftConfigInformer := kubeinformers.NewSharedInformerFactoryWithOptions(
		s.kubeClient, 0, kubeinformers.WithNamespace(defaults.OpenShiftConfigNamespace),
	)
	openshiftConfigManagedInformer := kubeinformers.NewSharedInformerFactoryWithOptions(
		s.kubeClient, 0, kubeinformers.WithNamespace(defaults.OpenShiftConfigManagedNamespace),
	)
	kubeSystemInformer := kubeinformers.NewSharedInformerFactoryWithOptions(
		s.kubeClient, 0, kubeinformers.WithNamespace("kube-system"),
	)

	var err error
	s.controller, err = NewController(
		events.NewInMemoryRecorder("test", clock.RealClock{}),
		&restclient.Config{},
		s.kubeClient,
		s.configClient,
		s.regClient,
		s.routeClient,
		s.kubeInformerFactory,
		openshiftConfigInformer,
		openshiftConfigManagedInformer,
		kubeSystemInformer,
		s.configInformerFactory,
		s.imageregistryInformerFactory,
		s.routeInformerFactory,
		nil,
		s.apiLister,
	)
	if err != nil {
		t.Fatalf("failed creating controller: %v", err)
	}

	s.kubeInformerFactory.Start(ctx.Done())
	s.configInformerFactory.Start(ctx.Done())
	s.imageregistryInformerFactory.Start(ctx.Done())
	s.routeInformerFactory.Start(ctx.Done())
	openshiftConfigInformer.Start(ctx.Done())
	openshiftConfigManagedInformer.Start(ctx.Done())
	kubeSystemInformer.Start(ctx.Done())

	s.kubeInformerFactory.WaitForCacheSync(ctx.Done())
	s.configInformerFactory.WaitForCacheSync(ctx.Done())
	s.imageregistryInformerFactory.WaitForCacheSync(ctx.Done())
	s.routeInformerFactory.WaitForCacheSync(ctx.Done())
	openshiftConfigInformer.WaitForCacheSync(ctx.Done())
	openshiftConfigManagedInformer.WaitForCacheSync(ctx.Done())
	kubeSystemInformer.WaitForCacheSync(ctx.Done())

	if !startRunLoop {
		return
	}

	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stopCh)
	}()
	go s.controller.Run(stopCh)
}

func TestGlobalTLSCopy(t *testing.T) {
	ctx := t.Context()
	setup := newTestControllerSetup(t)

	// apiServerConfig is the initial apiserver configuration using the
	// intermediate tls configuration set. add it to the client.
	apiServerConfig := &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type:         configv1.TLSProfileIntermediateType,
				Intermediate: &configv1.IntermediateTLSProfile{},
			},
		},
	}
	if err := setup.configClient.Tracker().Add(apiServerConfig); err != nil {
		t.Fatalf("failed to add api server to tracker: %v", err)
	}

	// add an initial registry configuration without populating its
	// observedConfig.
	if err := setup.regClient.Tracker().Add(&imageregistryapiv1.Config{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			Replicas: 1,
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Managed,
			},
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
		},
	}); err != nil {
		t.Fatalf("failed to add registry config to tracker: %v", err)
	}

	// start the controller and informers. both the ir controller and the
	// tls observer controller are started.
	configObserverController := configobserver.NewConfigObserver(
		"ImageRegistryConfigObserver",
		setup.operatorClient,
		events.NewInMemoryRecorder("test", clock.RealClock{}),
		setup.apiLister,
		[]factory.Informer{
			setup.configInformerFactory.Config().V1().APIServers().Informer(),
			setup.operatorClient.Informer(),
		},
		apiserver.ObserveTLSSecurityProfile,
	)
	setup.start(t, ctx, true)
	go configObserverController.Run(ctx, 1)

	// env helps to evaluate a given environment variable value.
	envValue := func(where []corev1.EnvVar, name string) (string, bool) {
		for _, env := range where {
			if env.Name == name {
				return env.Value, true
			}
		}
		return "", false
	}

	// wait until the environment variable for the tls version is set in
	// the image registry deployment.
	if err := wait.PollUntilContextTimeout(
		ctx, 100*time.Millisecond, time.Second, true,
		func(ctx context.Context) (bool, error) {
			deploy, err := setup.kubeClient.AppsV1().Deployments(
				defaults.ImageRegistryOperatorNamespace,
			).Get(ctx, "image-registry", metav1.GetOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					return false, err
				}
				t.Logf("image-registry deployment not found")
				return false, nil
			}
			val, exists := envValue(
				deploy.Spec.Template.Spec.Containers[0].Env,
				"REGISTRY_HTTP_TLS_MINVERSION",
			)
			t.Logf("REGISTRY_HTTP_TLS_MINVERSION found: %v, value: %q ", exists, val)
			return exists, nil
		},
	); err != nil {
		t.Fatalf("expected deployment to have REGISTRY_HTTP_TLS_MINVERSION: %v", err)
	}

	// now update the apiserver and set the tls to v1.3, this should
	// cascade and we expect this to be set in the deployment.
	apiServerConfig.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
		Type:   configv1.TLSProfileModernType,
		Modern: &configv1.ModernTLSProfile{},
	}
	if _, err := setup.configClient.ConfigV1().APIServers().Update(
		ctx, apiServerConfig, metav1.UpdateOptions{},
	); err != nil {
		t.Fatalf("failed to update APIServer with Modern TLS profile: %v", err)
	}

	// wait for the controller to automatically detect the config update
	// via its informer and reconcile the deployment with the modern
	// tls profile.
	if err := wait.PollUntilContextTimeout(
		ctx, 100*time.Millisecond, time.Second, true,
		func(ctx context.Context) (bool, error) {
			deployment, err := setup.kubeClient.AppsV1().Deployments(
				defaults.ImageRegistryOperatorNamespace,
			).Get(ctx, "image-registry", metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			val, exists := envValue(
				deployment.Spec.Template.Spec.Containers[0].Env,
				"REGISTRY_HTTP_TLS_MINVERSION",
			)
			t.Logf("REGISTRY_HTTP_TLS_MINVERSION found: %v, value: %q ", exists, val)
			return val == "VersionTLS13", nil
		},
	); err != nil {
		t.Fatalf("expected REGISTRY_HTTP_TLS_MINVERSION to be updated: %v", err)
	}
}
