package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapiv1 "github.com/openshift/api/operator/v1"

	imageregistryapiv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestPodResourceConfiguration(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: 1,
			Resources: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			NodeSelector: map[string]string{
				"node-role.kubernetes.io/master": "",
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "node-role.kubernetes.io/master",
					Operator: "Exists",
					Effect:   "NoSchedule",
				},
			},
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	deployments, err := client.Deployments(imageregistryapiv1.ImageRegistryOperatorNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments.Items) == 0 {
		t.Errorf("no deployments found in registry namespace")
	}

	for _, deployment := range deployments.Items {
		if strings.HasPrefix(deployment.Name, "image-registry") {
			mem, ok := deployment.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]
			if !ok {
				framework.DumpYAML(t, "deployment", deployment)
				t.Errorf("no memory limit set on registry deployment")
			}
			if mem.String() != "512Mi" {
				t.Errorf("expected memory limit of 512Mi, found: %s", mem.String())
			}
		}

	}
}

func TestPodTolerationsConfiguration(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	tolerations := []corev1.Toleration{
		{
			Key:      "mykey",
			Value:    "myvalue",
			Effect:   "NoSchedule",
			Operator: "Equal",
		},
	}

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas:    1,
			Tolerations: tolerations,
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	deployment, err := client.Deployments(imageregistryapiv1.ImageRegistryOperatorNamespace).Get("image-registry", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(tolerations, deployment.Spec.Template.Spec.Tolerations) {
		t.Errorf("expected tolerations not found wanted: %#v, got %#v", tolerations, deployment.Spec.Template.Spec.Tolerations)
	}

}

func TestRouteConfiguration(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	hostname := "test.example.com"

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas:     1,
			DefaultRoute: true,
			Routes: []imageregistryapiv1.ImageRegistryConfigRoute{
				{
					Name:     "testroute",
					Hostname: hostname,
				},
			},
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureDefaultExternalRegistryHostnameIsSet(t, client)
	framework.EnsureExternalRegistryHostnamesAreSet(t, client, []string{hostname})
	framework.MustEnsureDefaultExternalRouteExists(t, client)
	framework.EnsureExternalRoutesExist(t, client, []string{hostname})
}

func generateCertificate(hostname string) ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %s", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %s", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Example Ltd"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %s", err)
	}

	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	key := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return cert, key, nil
}

func TestSecureRouteConfiguration(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	hostname := "test.example.com"
	cert, key, err := generateCertificate(hostname)
	if err != nil {
		t.Fatal(err)
	}
	routeName := "testroute"
	tlsSecretName := "testroute-tls"
	tlsSecretData := map[string]string{
		"tls.crt": string(cert),
		"tls.key": string(key),
	}

	if _, err := util.CreateOrUpdateSecret(tlsSecretName, imageregistryapiv1.ImageRegistryOperatorNamespace, tlsSecretData); err != nil {
		t.Fatalf("unable to create secret: %s", err)
	}

	framework.MustDeployImageRegistry(t, client, &imageregistryapiv1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: 1,
			Routes: []imageregistryapiv1.ImageRegistryConfigRoute{
				{
					Name:       routeName,
					Hostname:   hostname,
					SecretName: tlsSecretName,
				},
			},
		},
	})
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.EnsureExternalRegistryHostnamesAreSet(t, client, []string{hostname})

	route, err := client.Routes(imageregistryapiv1.ImageRegistryOperatorNamespace).Get(routeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unable to get route: %s", err)
	}
	if route.Spec.TLS == nil {
		t.Fatal("route.Spec.TLS is nil, want a configuration")
	}
	if route.Spec.TLS.Certificate != string(cert) {
		t.Errorf("route tls certificate: got %q, want %q", route.Spec.TLS.Certificate, string(cert))
	}
	if route.Spec.TLS.Key != string(key) {
		t.Errorf("route tls key: got %q, want %q", route.Spec.TLS.Key, string(key))
	}
}

func TestVersionReporting(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: 1,
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	if _, err := client.Deployments(framework.OperatorDeploymentNamespace).Patch(framework.OperatorDeploymentName, types.StrategicMergePatchType, []byte(`{"spec": {"template": {"spec": {"containers": [{"name":"cluster-image-registry-operator","env":[{"name":"RELEASE_VERSION","value":"test-v2"}]}]}}}}`)); err != nil {
		t.Fatalf("failed to patch operator to new version: %v", err)
	}

	err := wait.Poll(5*time.Second, 1*time.Minute, func() (bool, error) {
		clusterOperatorStatus, err := client.ClusterOperators().Get(imageregistryapiv1.ImageRegistryClusterOperatorResourceName, metav1.GetOptions{})
		if err != nil {
			t.Logf("Could not retrieve cluster operator status: %v", err)
			return false, nil
		}
		if len(clusterOperatorStatus.Status.Versions) == 0 {
			// We should always have *some* version information in the clusteroperator once we are avaiable,
			// so we do not retry in this scenario.
			t.Fatalf("Cluster operator status has no version information: %v", clusterOperatorStatus)
			return true, err
		}
		if clusterOperatorStatus.Status.Versions[0].Name != "operator" || clusterOperatorStatus.Status.Versions[0].Version != "test-v2" {
			t.Logf("waiting for new version to be reported, saw: %v", clusterOperatorStatus.Status.Versions[0])
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("failed to observe updated version reported in clusteroperator status: %v", err)
	}
}

func TestRequests(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Requests: imageregistryapiv1.ImageRegistryConfigRequests{
				Read: imageregistryapiv1.ImageRegistryConfigRequestsLimits{
					MaxRunning: 1,
					MaxInQueue: 2,
					MaxWaitInQueue: metav1.Duration{
						Duration: 3 * time.Second,
					},
				},
				Write: imageregistryapiv1.ImageRegistryConfigRequestsLimits{
					MaxRunning: 4,
					MaxInQueue: 5,
					MaxWaitInQueue: metav1.Duration{
						Duration: 6 * time.Hour,
					},
				},
			},
			Replicas: 1,
		},
	}

	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)

	deploy, err := client.Deployments(imageregistryapiv1.ImageRegistryOperatorNamespace).Get(imageregistryapiv1.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	expectedEnvVars := []corev1.EnvVar{
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXRUNNING", Value: "1", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXINQUEUE", Value: "2", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXWAITINQUEUE", Value: "3s", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXRUNNING", Value: "4", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXINQUEUE", Value: "5", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXWAITINQUEUE", Value: "6h0m0s", ValueFrom: nil},
	}

	for _, err = range framework.CheckEnvVars(expectedEnvVars, deploy.Spec.Template.Spec.Containers[0].Env) {
		t.Errorf("%v", err)
	}
}
