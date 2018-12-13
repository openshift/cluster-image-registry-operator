package resource

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

func generateLogLevel(cr *v1alpha1.ImageRegistry) string {
	switch cr.Spec.Logging.Level {
	case 0:
		return "error"
	case 1:
		return "warn"
	case 2, 3:
		return "info"
	}
	return "debug"
}

func generateLivenessProbeConfig(cr *v1alpha1.ImageRegistry, p *parameters.Globals) *corev1.Probe {
	probeConfig := generateProbeConfig(cr, p)
	probeConfig.InitialDelaySeconds = 10

	return probeConfig
}

func generateReadinessProbeConfig(cr *v1alpha1.ImageRegistry, p *parameters.Globals) *corev1.Probe {
	return generateProbeConfig(cr, p)
}

func generateProbeConfig(cr *v1alpha1.ImageRegistry, p *parameters.Globals) *corev1.Probe {
	var scheme corev1.URIScheme
	if cr.Spec.TLS {
		scheme = corev1.URISchemeHTTPS
	}
	return &corev1.Probe{
		TimeoutSeconds: int32(p.Healthz.TimeoutSeconds),
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: scheme,
				Path:   p.Healthz.Route,
				Port:   intstr.FromInt(p.Container.Port),
			},
		},
	}
}

func generateSecurityContext(coreClient coreset.CoreV1Interface, cr *v1alpha1.ImageRegistry, namespace string) (*corev1.PodSecurityContext, error) {
	ns, err := coreClient.Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	sgrange, ok := ns.Annotations[parameters.SupplementalGroupsAnnotation]
	if !ok {
		return nil, fmt.Errorf("namespace %q doesn't have annotation %s", namespace, parameters.SupplementalGroupsAnnotation)
	}

	idx := strings.Index(sgrange, "/")
	if idx == -1 {
		return nil, fmt.Errorf("annotation %s in namespace %q doesn't contain '/'", parameters.SupplementalGroupsAnnotation, namespace)
	}

	gid, err := strconv.ParseInt(sgrange[:idx], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse annotation %s in namespace %q: %s", parameters.SupplementalGroupsAnnotation, namespace, err)
	}

	return &corev1.PodSecurityContext{
		FSGroup: &gid,
	}, nil
}

func storageConfigure(crname string, crnamespace string, cfg *v1alpha1.ImageRegistryConfigStorage) (envs []corev1.EnvVar, volumes []corev1.Volume, mounts []corev1.VolumeMount, err error) {
	var driver storage.Driver

	driver, err = storage.NewDriver(crname, crnamespace, cfg)
	if err != nil {
		return
	}

	envs, err = driver.ConfigEnv()
	if err != nil {
		return
	}

	volumes, mounts, err = driver.Volumes()
	if err != nil {
		return
	}

	return
}

func makePodTemplateSpec(coreClient coreset.CoreV1Interface, params *parameters.Globals, cr *v1alpha1.ImageRegistry) (corev1.PodTemplateSpec, *dependencies, error) {
	env, volumes, mounts, err := storageConfigure(cr.Name, params.Deployment.Namespace, &cr.Spec.Storage)
	if err != nil {
		return corev1.PodTemplateSpec{}, nil, err
	}

	deps := newDependencies()
	for _, e := range env {
		if e.ValueFrom == nil {
			continue
		}
		if e.ValueFrom.ConfigMapKeyRef != nil {
			deps.AddConfigMap(e.ValueFrom.ConfigMapKeyRef.Name)
		}
		if e.ValueFrom.SecretKeyRef != nil {
			deps.AddSecret(e.ValueFrom.SecretKeyRef.Name)
		}
	}

	env = append(env,
		corev1.EnvVar{Name: "REGISTRY_HTTP_ADDR", Value: fmt.Sprintf(":%d", params.Container.Port)},
		corev1.EnvVar{Name: "REGISTRY_HTTP_NET", Value: "tcp"},
		corev1.EnvVar{Name: "REGISTRY_HTTP_SECRET", Value: cr.Spec.HTTPSecret},
		corev1.EnvVar{Name: "REGISTRY_LOG_LEVEL", Value: generateLogLevel(cr)},
		corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_QUOTA_ENABLED", Value: "true"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_CACHE_BLOBDESCRIPTOR", Value: "inmemory"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_DELETE_ENABLED", Value: "true"},
		// TODO(dmage): sync with InternalRegistryHostname in origin
		corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_SERVER_ADDR", Value: fmt.Sprintf("%s.%s.svc:%d", params.Service.Name, params.Deployment.Namespace, params.Container.Port)},
	)

	if cr.Spec.Proxy.HTTP != "" {
		env = append(env, corev1.EnvVar{Name: "HTTP_PROXY", Value: cr.Spec.Proxy.HTTP})
	}

	if cr.Spec.Proxy.HTTPS != "" {
		env = append(env, corev1.EnvVar{Name: "HTTPS_PROXY", Value: cr.Spec.Proxy.HTTPS})
	}

	if cr.Spec.Proxy.NoProxy != "" {
		env = append(env, corev1.EnvVar{Name: "NO_PROXY", Value: cr.Spec.Proxy.NoProxy})
	}

	if cr.Spec.Requests.Read.MaxRunning != 0 || cr.Spec.Requests.Read.MaxInQueue != 0 {
		if cr.Spec.Requests.Read.MaxRunning < 0 {
			return corev1.PodTemplateSpec{}, deps, fmt.Errorf("Requests.Read.MaxRunning must be positive number")
		}
		if cr.Spec.Requests.Read.MaxInQueue < 0 {
			return corev1.PodTemplateSpec{}, deps, fmt.Errorf("Requests.Read.MaxInQueue must be positive number")
		}
		env = append(env,
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXRUNNING", Value: fmt.Sprintf("%d", cr.Spec.Requests.Read.MaxRunning)},
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXINQUEUE", Value: fmt.Sprintf("%d", cr.Spec.Requests.Read.MaxInQueue)},
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXWAITINQUEUE", Value: fmt.Sprintf("%s", cr.Spec.Requests.Read.MaxWaitInQueue)},
		)
	}

	if cr.Spec.Requests.Write.MaxRunning != 0 || cr.Spec.Requests.Write.MaxInQueue != 0 {
		if cr.Spec.Requests.Write.MaxRunning < 0 {
			return corev1.PodTemplateSpec{}, deps, fmt.Errorf("Requests.Write.MaxRunning must be positive number")
		}
		if cr.Spec.Requests.Write.MaxInQueue < 0 {
			return corev1.PodTemplateSpec{}, deps, fmt.Errorf("Requests.Write.MaxInQueue must be positive number")
		}
		env = append(env,
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXRUNNING", Value: fmt.Sprintf("%d", cr.Spec.Requests.Write.MaxRunning)},
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXINQUEUE", Value: fmt.Sprintf("%d", cr.Spec.Requests.Write.MaxInQueue)},
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXWAITINQUEUE", Value: fmt.Sprintf("%s", cr.Spec.Requests.Write.MaxWaitInQueue)},
		)
	}

	securityContext, err := generateSecurityContext(coreClient, cr, params.Deployment.Namespace)
	if err != nil {
		return corev1.PodTemplateSpec{}, deps, fmt.Errorf("generate security context for deployment config: %s", err)
	}

	//TLS
	if cr.Spec.TLS {
		vol := corev1.Volume{
			Name: "registry-tls",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: cr.ObjectMeta.Name + "-tls",
								},
							},
						},
					},
				},
			},
		}
		volumes = append(volumes, vol)
		mounts = append(mounts, corev1.VolumeMount{Name: vol.Name, MountPath: "/etc/secrets"})
		deps.AddSecret(vol.VolumeSource.Projected.Sources[0].Secret.LocalObjectReference.Name)

		env = append(env,
			corev1.EnvVar{Name: "REGISTRY_HTTP_TLS_CERTIFICATE", Value: "/etc/secrets/tls.crt"},
			corev1.EnvVar{Name: "REGISTRY_HTTP_TLS_KEY", Value: "/etc/secrets/tls.key"},
		)
	}

	// Certificates
	vol := corev1.Volume{
		Name: "registry-certificates",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cr.ObjectMeta.Name + "-certificates",
				},
			},
		},
	}
	volumes = append(volumes, vol)
	mounts = append(mounts, corev1.VolumeMount{Name: vol.Name, MountPath: "/etc/pki/ca-trust/source/anchors"})
	deps.AddConfigMap(vol.VolumeSource.ConfigMap.LocalObjectReference.Name)

	image := os.Getenv("IMAGE")

	spec := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: params.Deployment.Labels,
		},
		Spec: corev1.PodSpec{
			Tolerations: []corev1.Toleration{
				{
					Key:      "node-role.kubernetes.io/master",
					Operator: "Exists",
					Effect:   "NoSchedule",
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "registry",
					Image: image,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(params.Container.Port),
							Protocol:      "TCP",
						},
					},
					Env:            env,
					VolumeMounts:   mounts,
					LivenessProbe:  generateLivenessProbeConfig(cr, params),
					ReadinessProbe: generateReadinessProbeConfig(cr, params),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
			Volumes:            volumes,
			ServiceAccountName: params.Pod.ServiceAccount,
			SecurityContext:    securityContext,
		},
	}

	return spec, deps, nil
}
