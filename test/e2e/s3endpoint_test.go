package e2e

import (
	"context"
	"fmt"
	"testing"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type CleanupFunc func()

type Cleanup struct {
	funcs []CleanupFunc
	done  bool // true if Func has been called
}

func (c *Cleanup) Add(f CleanupFunc) {
	c.funcs = append(c.funcs, f)
}

func (c *Cleanup) Func() CleanupFunc {
	c.done = true
	return func() {
		for _, f := range c.funcs {
			defer f()
		}
	}
}

func (c *Cleanup) Defer() {
	if !c.done {
		for _, f := range c.funcs {
			defer f()
		}
		c.funcs = nil
	}
}

func deployMinio(ctx context.Context, te framework.TestEnv) (minioEndpoint string, minioAccessKey string, minioSecretKey string, minioCAConfigMapName string, cleanupFunc CleanupFunc) {
	var cleanup Cleanup
	defer cleanup.Defer()

	const caConfigMapName = "e2e-image-registry-s3-minio-ca"
	const nsName = "e2e-image-registry-s3-minio"
	const accessKey = "accesskey"
	const secretKey = "secretkey"

	caCert, caKey, err := framework.GenerateX509RootCA()
	if err != nil {
		te.Fatalf("failed to generate CA: %s", err)
	}

	hostname := fmt.Sprintf("minio.%s.svc", nsName)

	cert, key, err := framework.GenerateX509Certificate(hostname, caCert, caKey)
	if err != nil {
		te.Fatalf("failed to generate certificate: %s", err)
	}

	te.Logf("creating namespace %s...", nsName)
	ns, err := te.Client().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		te.Fatalf("failed to create test namespace: %v", err)
	}
	cleanup.Add(func() {
		te.Logf("deleting namespace %s...", nsName)
		err = te.Client().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
		if err != nil {
			te.Errorf("failed to delete namespace %s: %v", ns.Name, err)
		}
	})

	te.Logf("creating config map %s...", caConfigMapName)
	_, err = te.Client().ConfigMaps("openshift-config").Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: caConfigMapName,
		},
		Data: map[string]string{
			"ca-bundle.crt": string(framework.EncodeX509Certificate(caCert)),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		te.Fatalf("failed to create config map: %s", err)
	}
	cleanup.Add(func() {
		te.Logf("deleting config map %s...", caConfigMapName)
		if err := te.Client().ConfigMaps("openshift-config").Delete(ctx, caConfigMapName, metav1.DeleteOptions{}); err != nil {
			te.Errorf("failed to delete config map %s: %w", caConfigMapName, err)
		}
	})

	te.Logf("creating Minio certs...")
	_, err = te.Client().Secrets(nsName).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "minio-certs",
		},
		Data: map[string][]byte{
			"public.crt":  cert,
			"private.key": key,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		te.Fatalf("failed to create certs secret: %s", err)
	}

	te.Logf("creating Minio deployment...")
	replicas := int32(1)
	runAsNonRoot := true
	allowPrivilegeEscalation := false
	_, err = te.Client().Deployments(nsName).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "minio",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "minio",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "minio",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "minio",
							Image: "minio/minio:RELEASE.2022-03-26T06-49-28Z",
							Args: []string{
								"minio",
								"--certs-dir=/certs",
								"server",
								"/data",
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 9000,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "minio-certs",
									MountPath: "/certs/public.crt",
									SubPath:   "public.crt",
								},
								{
									Name:      "minio-certs",
									MountPath: "/certs/private.key",
									SubPath:   "private.key",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "MINIO_ROOT_USER",
									Value: accessKey,
								},
								{
									Name:  "MINIO_ROOT_PASSWORD",
									Value: secretKey,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								RunAsNonRoot:             &runAsNonRoot,
								AllowPrivilegeEscalation: &allowPrivilegeEscalation,
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "minio-certs",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "minio-certs",
								},
							},
						},
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		te.Fatalf("failed to create deployment: %s", err)
	}

	_, err = te.Client().Services(nsName).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "minio",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       443,
					TargetPort: intstr.FromInt(9000),
				},
			},
			Selector: map[string]string{
				"app": "minio",
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		te.Fatalf("failed to create service: %s", err)
	}

	return "https://" + hostname, accessKey, secretKey, caConfigMapName, cleanup.Func()
}

func TestS3Minio(t *testing.T) {
	ctx := context.Background()

	te := framework.Setup(t)

	te.Logf("deploying Minio...")
	minioEndpoint, accessKey, secretKey, caConfigMapName, cleanup := deployMinio(ctx, te)
	defer cleanup()

	defer framework.TeardownImageRegistry(te)

	_, err := te.Client().Secrets("openshift-image-registry").Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "image-registry-private-configuration-user",
		},
		Data: map[string][]byte{
			"REGISTRY_STORAGE_S3_ACCESSKEY": []byte(accessKey),
			"REGISTRY_STORAGE_S3_SECRETKEY": []byte(secretKey),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		te.Fatalf("failed to create registry secrets: %s", err)
	}
	defer func() {
		if err := te.Client().Secrets("openshift-image-registry").Delete(ctx, "image-registry-private-configuration-user", metav1.DeleteOptions{}); err != nil {
			te.Errorf("failed to delete registry secrets: %s", err)
		}
	}()

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Replicas: 1,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			S3: &imageregistryv1.ImageRegistryConfigStorageS3{
				Region:         "us-east-1",
				RegionEndpoint: minioEndpoint,
				TrustedCA: imageregistryv1.S3TrustedCASource{
					Name: caConfigMapName,
				},
			},
		},
	})
	framework.WaitUntilImageRegistryIsAvailable(te)
}
