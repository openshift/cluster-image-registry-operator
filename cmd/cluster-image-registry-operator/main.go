package main

import (
	"context"
	"runtime"

	kappsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	appsapi "github.com/openshift/api/apps/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/version"
	sdk "github.com/operator-framework/operator-sdk/pkg/sdk"
	k8sutil "github.com/operator-framework/operator-sdk/pkg/util/k8sutil"
	sdkVersion "github.com/operator-framework/operator-sdk/version"

	"github.com/sirupsen/logrus"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func printVersion() {
	logrus.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	logrus.Infof("Go Version: %s", runtime.Version())
	logrus.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	logrus.Infof("operator-sdk Version: %v", sdkVersion.Version)
}

func watch(apiVersion, kind, namespace string, resyncPeriod int) {
	logrus.Infof("Watching %s, %s, %s, %d", apiVersion, kind, namespace, resyncPeriod)
	sdk.Watch(apiVersion, kind, namespace, resyncPeriod)
}

func main() {
	printVersion()

	sdk.ExposeMetricsPort()

	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		logrus.Fatalf("failed to get watch namespace: %s", err)
	}

	handler, err := operator.NewHandler(namespace)
	if err != nil {
		logrus.Fatal(err)
	}

	k8sutil.AddToSDKScheme(appsapi.AddToScheme)

	watch(corev1.SchemeGroupVersion.String(), "ConfigMap", namespace, 0)
	watch(corev1.SchemeGroupVersion.String(), "Secret", namespace, 0)
	watch(kappsapi.SchemeGroupVersion.String(), "Deployment", namespace, 0)
	watch(appsapi.SchemeGroupVersion.String(), "DeploymentConfig", namespace, 0)
	watch(regopapi.SchemeGroupVersion.String(), "OpenShiftDockerRegistry", namespace, 5)

	sdk.Handle(handler)
	sdk.Run(context.TODO())
}
