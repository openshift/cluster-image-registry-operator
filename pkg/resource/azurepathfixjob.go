package resource

import (
	"context"
	"fmt"
	"os"
	"reflect"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kcorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	batchset "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	securityv1 "github.com/openshift/api/security/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure"
)

var _ Mutator = &generatorAzurePathFixJob{}

type generatorAzurePathFixJob struct {
	lister                batchlisters.JobNamespaceLister
	secretLister          corev1listers.SecretNamespaceLister
	infrastructureLister  configlisters.InfrastructureLister
	proxyLister           configlisters.ProxyLister
	openshiftConfigLister corev1listers.ConfigMapNamespaceLister
	client                batchset.BatchV1Interface
	cr                    *imageregistryv1.Config
	kubeconfig            *restclient.Config
}

func NewGeneratorAzurePathFixJob(
	lister batchlisters.JobNamespaceLister,
	client batchset.BatchV1Interface,
	secretLister corev1listers.SecretNamespaceLister,
	infrastructureLister configlisters.InfrastructureLister,
	proxyLister configlisters.ProxyLister,
	openshiftConfigLister corev1listers.ConfigMapNamespaceLister,
	cr *imageregistryv1.Config,
	kubeconfig *restclient.Config,
) *generatorAzurePathFixJob {
	return &generatorAzurePathFixJob{
		lister:                lister,
		client:                client,
		cr:                    cr,
		infrastructureLister:  infrastructureLister,
		secretLister:          secretLister,
		proxyLister:           proxyLister,
		openshiftConfigLister: openshiftConfigLister,
		kubeconfig:            kubeconfig,
	}
}

func (gapfj *generatorAzurePathFixJob) Type() runtime.Object {
	return &batchv1.Job{}
}

func (gapfj *generatorAzurePathFixJob) GetNamespace() string {
	return defaults.ImageRegistryOperatorNamespace
}

func (gapfj *generatorAzurePathFixJob) GetName() string {
	return "azure-path-fix"
}

func (gapfj *generatorAzurePathFixJob) expected() (runtime.Object, error) {
	azureCfg, err := azure.GetConfig(gapfj.secretLister, gapfj.infrastructureLister)
	if err != nil {
		return nil, err
	}
	clusterProxy, err := gapfj.proxyLister.Get(defaults.ClusterProxyResourceName)
	if errors.IsNotFound(err) {
		clusterProxy = &configapiv1.Proxy{}
	} else if err != nil {
		// TODO: should we report Degraded?
		return nil, fmt.Errorf("unable to get cluster proxy configuration: %v", err)
	}

	azureStorage := gapfj.cr.Status.Storage.Azure
	if azureStorage == nil {
		// TODO: should we return a custom error here, and treat it like a non error in the controller?
		// this is expected when the operator is set to Removed (after removing check if storage is
		// configured from the controller).
		return nil, fmt.Errorf("storage not yet provisioned")
	}

	optional := true
	envs := []corev1.EnvVar{
		{Name: "AZURE_ENVIRONMENT_FILEPATH", Value: os.Getenv("AZURE_ENVIRONMENT_FILEPATH")},
		{Name: "AZURE_STORAGE_ACCOUNT_NAME", Value: azureStorage.AccountName},
		{Name: "AZURE_CONTAINER_NAME", Value: azureStorage.Container},
		{Name: "AZURE_CLIENT_ID", Value: azureCfg.ClientID},
		{Name: "AZURE_TENANT_ID", Value: azureCfg.TenantID},
		{Name: "AZURE_CLIENT_SECRET", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				Optional: &optional,
				LocalObjectReference: corev1.LocalObjectReference{
					Name: defaults.CloudCredentialsName,
				},
				Key: "azure_client_secret",
			},
		}},
		{Name: "AZURE_FEDERATED_TOKEN_FILE", Value: azureCfg.FederatedTokenFile},
		{Name: "AZURE_ACCOUNTKEY", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				Optional: &optional,
				LocalObjectReference: corev1.LocalObjectReference{
					Name: defaults.ImageRegistryPrivateConfiguration,
				},
				Key: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
			},
		}},
	}

	// for Azure Stack Hub, the move-blobs command needs to know the endpoints,
	// and those come from the cloud-provider-config in the openshift-config
	// namespace.
	cm, err := gapfj.openshiftConfigLister.Get("cloud-provider-config")
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	if cm != nil {
		envs = append(envs, corev1.EnvVar{Name: "AZURE_ENVIRONMENT_FILECONTENTS", Value: cm.Data["endpoints"]})
	}

	if len(azureStorage.CloudName) > 0 {
		envs = append(envs, corev1.EnvVar{Name: "AZURE_ENVIRONMENT", Value: azureStorage.CloudName})
	}

	if gapfj.cr.Spec.Proxy.HTTP != "" {
		envs = append(envs, corev1.EnvVar{Name: "HTTP_PROXY", Value: gapfj.cr.Spec.Proxy.HTTP})
	} else if clusterProxy.Status.HTTPProxy != "" {
		envs = append(envs, corev1.EnvVar{Name: "HTTP_PROXY", Value: clusterProxy.Status.HTTPProxy})
	}

	if gapfj.cr.Spec.Proxy.HTTPS != "" {
		envs = append(envs, corev1.EnvVar{Name: "HTTPS_PROXY", Value: gapfj.cr.Spec.Proxy.HTTPS})
	} else if clusterProxy.Status.HTTPSProxy != "" {
		envs = append(envs, corev1.EnvVar{Name: "HTTPS_PROXY", Value: clusterProxy.Status.HTTPSProxy})
	}

	if gapfj.cr.Spec.Proxy.NoProxy != "" {
		envs = append(envs, corev1.EnvVar{Name: "NO_PROXY", Value: gapfj.cr.Spec.Proxy.NoProxy})
	} else if clusterProxy.Status.NoProxy != "" {
		envs = append(envs, corev1.EnvVar{Name: "NO_PROXY", Value: clusterProxy.Status.NoProxy})
	}

	// Cluster trusted certificate authorities - mount to /usr/share/pki/ca-trust-source/ to add
	// CAs as low-priority trust sources. Registry runs update-ca-trust extract on startup, which
	// merges the registry CAs with the cluster's trusted CAs into a single CA bundle.
	//
	// See man update-ca-trust for more information.
	optional = true
	trustedCAVolume := corev1.Volume{
		Name: "trusted-ca",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: defaults.TrustedCAName,
				},
				// Trust bundle is in PEM format - needs to be mounted to /anchors so that
				// update-ca-trust extract knows that these CAs should always be trusted.
				// This also ensures that no other low-priority trust is present in the container.
				//
				// See man update-ca-trust for more information.
				Items: []corev1.KeyToPath{
					{
						Key:  "ca-bundle.crt",
						Path: "anchors/ca-bundle.crt",
					},
				},
				Optional: &optional,
			},
		},
	}
	trustedCAMount := corev1.VolumeMount{
		Name:      trustedCAVolume.Name,
		MountPath: "/usr/share/pki/ca-trust-source",
	}
	caTrustExtractedVolume := corev1.Volume{
		Name: "ca-trust-extracted",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	caTrustExtractedMount := corev1.VolumeMount{
		Name:      "ca-trust-extracted",
		MountPath: "/etc/pki/ca-trust/extracted",
	}
	saVol := corev1.Volume{
		Name: "bound-sa-token",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Audience: "openshift",
							Path:     "token",
						},
					},
				},
			},
		},
	}
	saMount := corev1.VolumeMount{
		Name: saVol.Name,
		// Default (by convention) location for mounting projected ServiceAccounts
		MountPath: "/var/run/secrets/openshift/serviceaccount",
		ReadOnly:  true,
	}

	backoffLimit := int32(6)
	cj := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gapfj.GetName(),
			Namespace: gapfj.GetNamespace(),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: kcorev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						securityv1.RequiredSCCAnnotation: "restricted-v2",
					},
				},
				Spec: kcorev1.PodSpec{
					RestartPolicy:      kcorev1.RestartPolicyNever,
					ServiceAccountName: defaults.ServiceAccountName,
					PriorityClassName:  "system-cluster-critical",
					Containers: []kcorev1.Container{
						{
							Image: os.Getenv("OPERATOR_IMAGE"),
							Resources: kcorev1.ResourceRequirements{
								Requests: kcorev1.ResourceList{
									kcorev1.ResourceCPU:    resource.MustParse("100m"),
									kcorev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							TerminationMessagePolicy: kcorev1.TerminationMessageFallbackToLogsOnError,
							Env:                      envs,
							VolumeMounts: []corev1.VolumeMount{
								trustedCAMount,
								caTrustExtractedMount,
								saMount,
							},
							Name:    gapfj.GetName(),
							Command: []string{"/bin/sh"},
							Args: []string{
								"-c",
								"mkdir -p /etc/pki/ca-trust/extracted/edk2 /etc/pki/ca-trust/extracted/java /etc/pki/ca-trust/extracted/openssl /etc/pki/ca-trust/extracted/pem && update-ca-trust extract --output /etc/pki/ca-trust/extracted/ && /usr/bin/move-blobs",
							},
						},
					},
					Volumes: []corev1.Volume{
						trustedCAVolume,
						caTrustExtractedVolume,
						saVol,
					},
				},
			},
		},
	}

	return cj, nil
}

func (gapfj *generatorAzurePathFixJob) Get() (runtime.Object, error) {
	return gapfj.lister.Get(gapfj.GetName())
}

func (gapfj *generatorAzurePathFixJob) Create() (runtime.Object, error) {
	return commonCreate(gapfj, func(obj runtime.Object) (runtime.Object, error) {
		return gapfj.client.Jobs(gapfj.GetNamespace()).Create(
			context.TODO(), obj.(*batchv1.Job), metav1.CreateOptions{},
		)
	})
}

func (gapfj *generatorAzurePathFixJob) Update(o runtime.Object) (runtime.Object, bool, error) {
	// updating jobs doesn't work like other objects - we get validation errors
	// if we try. in our case, we only care about the job container's env vars,
	// so we check if the existing job's container env vars match the expected,
	// and if they don't we recreate the job.
	exp, err := gapfj.expected()
	if err != nil {
		return nil, false, err
	}
	expectedJob := exp.(*batchv1.Job)
	job := o.(*batchv1.Job)
	expectedEnvs := expectedJob.Spec.Template.Spec.Containers[0].Env
	actualEnvs := job.Spec.Template.Spec.Containers[0].Env

	if reflect.DeepEqual(expectedEnvs, actualEnvs) {
		return o, false, nil
	}

	// if we are here it means the expected container envs differed from
	// the actual container envs, so we recreate the job.
	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground
	opts := metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}
	if err := gapfj.Delete(opts); err != nil {
		return nil, false, err
	}
	createdObj, err := gapfj.Create()
	if err != nil {
		return nil, false, err
	}
	return createdObj, true, nil
}

func (gapfj *generatorAzurePathFixJob) Delete(opts metav1.DeleteOptions) error {
	return gapfj.client.Jobs(gapfj.GetNamespace()).Delete(
		context.TODO(), gapfj.GetName(), opts,
	)
}

func (gapfj *generatorAzurePathFixJob) Owned() bool {
	return true
}
