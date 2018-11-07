package main

import (
	"context"
	"flag"
	"os"
	"runtime"
	"time"

	kappsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacapi "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/openshift/api/apps/v1"
	routeapi "github.com/openshift/api/route/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/version"
	sdk "github.com/operator-framework/operator-sdk/pkg/sdk"
	k8sutil "github.com/operator-framework/operator-sdk/pkg/util/k8sutil"
	sdkVersion "github.com/operator-framework/operator-sdk/version"

	"github.com/sirupsen/logrus"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

var logLevel = flag.String("loglevel", "", "sets the sensitivity of logging output.")

func printVersion() {
	logrus.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	logrus.Infof("Go Version: %s", runtime.Version())
	logrus.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	logrus.Infof("operator-sdk Version: %v", sdkVersion.Version)
}

func watch(apiVersion, kind, namespace string, resyncPeriod time.Duration) {
	logrus.Infof("Watching %s, %s, %s, %d", apiVersion, kind, namespace, resyncPeriod)
	sdk.Watch(apiVersion, kind, namespace, resyncPeriod)
}

func main() {
	flag.Parse()

	if len(*logLevel) == 0 {
		envval := os.Getenv("LOG_LEVEL")
		if len(envval) > 0 {
			*logLevel = envval
		} else {
			*logLevel = "info"
		}
	}
	lvl, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatal(err)
	}

	logrus.SetLevel(lvl)

	printVersion()

	sdk.ExposeMetricsPort()

	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		logrus.Fatalf("failed to get watch namespace: %s", err)
	}

	k8sutil.AddToSDKScheme(appsapi.AddToScheme)
	k8sutil.AddToSDKScheme(routeapi.AddToScheme)

	dc := &appsapi.DeploymentConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsapi.SchemeGroupVersion.String(),
			Kind:       "DeploymentConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "docker-registry",
			Namespace: "default",
		},
	}
	err = sdk.Get(dc)
	if err != nil {
		if !errors.IsNotFound(err) {
			logrus.Fatal(err)
		}
	} else {
		namespace = "default"
	}

	handler, err := operator.NewHandler(namespace)
	if err != nil {
		logrus.Fatal(err)
	}

	watch(rbacapi.SchemeGroupVersion.String(), "ClusterRole", "", 0)
	watch(rbacapi.SchemeGroupVersion.String(), "ClusterRoleBinding", "", 0)
	watch(regopapi.SchemeGroupVersion.String(), "ImageRegistry", "", 10*time.Minute)

	watch(corev1.SchemeGroupVersion.String(), "ConfigMap", namespace, 0)
	watch(corev1.SchemeGroupVersion.String(), "Secret", namespace, 0)
	watch(corev1.SchemeGroupVersion.String(), "ServiceAccount", namespace, 0)
	watch(corev1.SchemeGroupVersion.String(), "Service", namespace, 0)
	watch(routeapi.SchemeGroupVersion.String(), "Route", namespace, 0)
	watch(kappsapi.SchemeGroupVersion.String(), "Deployment", namespace, 10*time.Minute)
	watch(appsapi.SchemeGroupVersion.String(), "DeploymentConfig", namespace, 10*time.Minute)

	sdk.Handle(handler)
	sdk.Run(context.TODO())
}
