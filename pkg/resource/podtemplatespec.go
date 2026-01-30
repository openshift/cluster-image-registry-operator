package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/ptr"

	configapiv1 "github.com/openshift/api/config/v1"
	v1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/crypto"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// generateLogLevel returns the appropriate operand log level according to user
// provided configuration.
func generateLogLevel(cr *v1.Config) string {
	switch cr.Spec.LogLevel {
	case operatorapiv1.Debug, operatorapiv1.Trace, operatorapiv1.TraceAll:
		return "debug"
	}

	switch cr.Spec.Logging {
	case 1:
		return "warn"
	case 0, 2, 3:
		return "info"
	}
	return "debug"
}

// generateTLSEnvVars extracts TLS configuration from observedConfig and returns
// environment variables for the registry container.
func generateTLSEnvVars(cr *v1.Config) ([]corev1.EnvVar, error) {
	var envVars []corev1.EnvVar

	if len(cr.Spec.ObservedConfig.Raw) == 0 {
		return envVars, nil
	}

	observedConfig := map[string]any{}
	if err := json.Unmarshal(cr.Spec.ObservedConfig.Raw, &observedConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal observedConfig: %w", err)
	}

	// extract minTLSVersion from servingInfo.minTLSVersion
	minTLSVersion, found, err := unstructured.NestedString(observedConfig, "servingInfo", "minTLSVersion")
	if err != nil {
		return nil, fmt.Errorf("failed to get servingInfo.minTLSVersion: %w", err)
	}
	if found && minTLSVersion != "" {
		if _, err := crypto.TLSVersion(minTLSVersion); err != nil {
			return nil, fmt.Errorf("invalid TLS version: %w", err)
		}
		envVars = append(
			envVars, corev1.EnvVar{
				Name:  "REGISTRY_HTTP_TLS_MINVERSION",
				Value: minTLSVersion,
			},
		)
	}

	// extract cipherSuites from servingInfo.cipherSuites
	cipherSuites, found, err := unstructured.NestedStringSlice(observedConfig, "servingInfo", "cipherSuites")
	if err != nil {
		return nil, fmt.Errorf("failed to get servingInfo.cipherSuites: %w", err)
	}

	if found && len(cipherSuites) > 0 {
		for _, cs := range cipherSuites {
			if _, err := crypto.CipherSuite(cs); err != nil {
				return nil, fmt.Errorf("invalid cipher suite: %w", err)
			}
		}
		envVars = append(
			envVars, corev1.EnvVar{
				Name:  "OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES",
				Value: strings.Join(cipherSuites, ","),
			},
		)
	}

	return envVars, nil
}

// generateLivenessProbeConfig returns an HTTPS liveness probe for the image
// registry.
func generateLivenessProbeConfig() *corev1.Probe {
	probeConfig := generateProbeConfig()
	// Wait until the registry is ready to serve requests.
	probeConfig.InitialDelaySeconds = 5
	return probeConfig
}

// generateReadinessProbeConfig returns an HTTPS readiness probe for the image
// registry.
func generateReadinessProbeConfig() *corev1.Probe {
	probeConfig := generateProbeConfig()
	// Wait until the registry checks its storage health before reporting
	// the registry as Ready.
	probeConfig.InitialDelaySeconds = 15
	return probeConfig
}

func generateProbeConfig() *corev1.Probe {
	return &corev1.Probe{
		TimeoutSeconds: int32(defaults.HealthzTimeoutSeconds),
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTPS,
				Path:   defaults.HealthzRoute,
				Port:   intstr.FromInt(defaults.ContainerPort),
			},
		},
	}
}

func generateSecurityContext(coreClient coreset.CoreV1Interface, namespace string) (*corev1.PodSecurityContext, error) {
	ns, err := coreClient.Namespaces().Get(
		context.TODO(), namespace, metav1.GetOptions{},
	)
	if err != nil {
		return nil, err
	}

	sgrange, ok := ns.Annotations[defaults.SupplementalGroupsAnnotation]
	if !ok {
		return nil, fmt.Errorf("namespace %q doesn't have annotation %s", namespace, defaults.SupplementalGroupsAnnotation)
	}

	idx := strings.Index(sgrange, "/")
	if idx == -1 {
		return nil, fmt.Errorf("annotation %s in namespace %q doesn't contain '/'", defaults.SupplementalGroupsAnnotation, namespace)
	}

	gid, err := strconv.ParseInt(sgrange[:idx], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse annotation %s in namespace %q: %s", defaults.SupplementalGroupsAnnotation, namespace, err)
	}

	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	return &corev1.PodSecurityContext{
		FSGroup:             &gid,
		FSGroupChangePolicy: &fsGroupChangePolicy,
	}, nil
}

func storageConfigure(driver storage.Driver) (envs []corev1.EnvVar, volumes []corev1.Volume, mounts []corev1.VolumeMount, err error) {
	configenvs, err := driver.ConfigEnv()
	if err != nil {
		return
	}

	envs, err = configenvs.EnvVars(defaults.ImageRegistryPrivateConfiguration)
	if err != nil {
		return
	}

	volumes, mounts, err = driver.Volumes()
	if err != nil {
		return
	}

	return
}

func makePodTemplateSpec(coreClient coreset.CoreV1Interface, proxyLister configlisters.ProxyLister, driver storage.Driver, cr *v1.Config) (corev1.PodTemplateSpec, *dependencies, error) {
	env, volumes, mounts, err := storageConfigure(driver)
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

	// If the storage driver is asking for specific volumes to be mounted in,
	// then ensure we redeploy on a change.
	for _, vol := range volumes {
		if vol.Secret != nil {
			deps.AddSecret(vol.Secret.SecretName)
		}
		if vol.ConfigMap != nil {
			deps.AddConfigMap(vol.ConfigMap.Name)
		}
	}

	clusterProxy, err := proxyLister.Get(defaults.ClusterProxyResourceName)
	if errors.IsNotFound(err) {
		clusterProxy = &configapiv1.Proxy{}
	} else if err != nil {
		// TODO: should we report Degraded?
		return corev1.PodTemplateSpec{}, deps, fmt.Errorf("unable to get cluster proxy configuration: %v", err)
	}

	env = append(env,
		corev1.EnvVar{Name: "REGISTRY_HTTP_ADDR", Value: fmt.Sprintf(":%d", defaults.ContainerPort)},
		corev1.EnvVar{Name: "REGISTRY_HTTP_NET", Value: "tcp"},
		corev1.EnvVar{Name: "REGISTRY_HTTP_SECRET", Value: cr.Spec.HTTPSecret},
		corev1.EnvVar{Name: "REGISTRY_LOG_LEVEL", Value: generateLogLevel(cr)},
		corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_QUOTA_ENABLED", Value: "true"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_CACHE_BLOBDESCRIPTOR", Value: "inmemory"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_DELETE_ENABLED", Value: "true"},
		corev1.EnvVar{Name: "REGISTRY_HEALTH_STORAGEDRIVER_ENABLED", Value: "true"},
		corev1.EnvVar{Name: "REGISTRY_HEALTH_STORAGEDRIVER_INTERVAL", Value: "10s"},
		corev1.EnvVar{Name: "REGISTRY_HEALTH_STORAGEDRIVER_THRESHOLD", Value: "1"},
		corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_METRICS_ENABLED", Value: "true"},
		// TODO(dmage): sync with InternalRegistryHostname in origin
		corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_SERVER_ADDR", Value: fmt.Sprintf("%s.%s.svc:%d", defaults.ServiceName, defaults.ImageRegistryOperatorNamespace, defaults.ContainerPort)},
	)

	if cr.Spec.ReadOnly {
		env = append(env, corev1.EnvVar{Name: "REGISTRY_STORAGE_MAINTENANCE_READONLY", Value: "{enabled: true}"})
	}

	if cr.Spec.DisableRedirect {
		env = append(env, corev1.EnvVar{Name: "REGISTRY_STORAGE_REDIRECT_DISABLE", Value: "true"})
	}

	if cr.Spec.Proxy.HTTP != "" {
		env = append(env, corev1.EnvVar{Name: "HTTP_PROXY", Value: cr.Spec.Proxy.HTTP})
	} else if clusterProxy.Status.HTTPProxy != "" {
		env = append(env, corev1.EnvVar{Name: "HTTP_PROXY", Value: clusterProxy.Status.HTTPProxy})
	}

	if cr.Spec.Proxy.HTTPS != "" {
		env = append(env, corev1.EnvVar{Name: "HTTPS_PROXY", Value: cr.Spec.Proxy.HTTPS})
	} else if clusterProxy.Status.HTTPSProxy != "" {
		env = append(env, corev1.EnvVar{Name: "HTTPS_PROXY", Value: clusterProxy.Status.HTTPSProxy})
	}

	if cr.Spec.Proxy.NoProxy != "" {
		env = append(env, corev1.EnvVar{Name: "NO_PROXY", Value: cr.Spec.Proxy.NoProxy})
	} else if clusterProxy.Status.NoProxy != "" {
		env = append(env, corev1.EnvVar{Name: "NO_PROXY", Value: clusterProxy.Status.NoProxy})
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
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXWAITINQUEUE", Value: cr.Spec.Requests.Read.MaxWaitInQueue.Duration.String()},
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
			corev1.EnvVar{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXWAITINQUEUE", Value: cr.Spec.Requests.Write.MaxWaitInQueue.Duration.String()},
		)
	}

	securityContext, err := generateSecurityContext(coreClient, defaults.ImageRegistryOperatorNamespace)
	if err != nil {
		return corev1.PodTemplateSpec{}, deps, fmt.Errorf("generate security context for deployment config: %s", err)
	}

	vol := corev1.Volume{
		Name: "registry-tls",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: defaults.ImageRegistryName + "-tls",
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

	// Add TLS version and cipher suites from observedConfig
	tlsEnvVars, err := generateTLSEnvVars(cr)
	if err != nil {
		return corev1.PodTemplateSpec{}, nil, fmt.Errorf("unable to generate tls config: %w", err)
	}
	env = append(env, tlsEnvVars...)

	volumes = append(volumes, corev1.Volume{
		Name: "ca-trust-extracted",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	mounts = append(mounts, corev1.VolumeMount{
		Name:      "ca-trust-extracted",
		MountPath: "/etc/pki/ca-trust/extracted",
	})

	// Registry certificate authorities - mount as high-priority trust source anchors
	vol = corev1.Volume{
		Name: "registry-certificates",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: defaults.ImageRegistryCertificatesName,
				},
			},
		},
	}
	volumes = append(volumes, vol)
	mounts = append(mounts, corev1.VolumeMount{Name: vol.Name, MountPath: "/etc/pki/ca-trust/source/anchors"})
	deps.AddConfigMap(defaults.ImageRegistryCertificatesName)

	// Cluster trusted certificate authorities - mount to /usr/share/pki/ca-trust-source/ to add
	// CAs as low-priority trust sources. Registry runs update-ca-trust extract on startup, which
	// merges the registry CAs with the cluster's trusted CAs into a single CA bundle.
	//
	// See man update-ca-trust for more information.
	optional := true
	vol = corev1.Volume{
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
	volumes = append(volumes, vol)
	mounts = append(mounts, corev1.VolumeMount{Name: vol.Name, MountPath: "/usr/share/pki/ca-trust-source"})
	deps.AddConfigMap(defaults.TrustedCAName)

	vol = corev1.Volume{
		Name: defaults.InstallationPullSecret,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				Items: []corev1.KeyToPath{
					{
						Key:  ".dockerconfigjson",
						Path: "config.json",
					},
				},
				SecretName: defaults.InstallationPullSecret,
				Optional:   &optional,
			},
		},
	}
	volumes = append(volumes, vol)
	mounts = append(
		mounts,
		corev1.VolumeMount{
			Name:      vol.Name,
			MountPath: "/var/lib/kubelet/",
		},
	)
	deps.AddSecret(defaults.InstallationPullSecret)

	// Project the ServiceAccount token into the Pod (for Pod-identity authentication cases).
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
	volumes = append(volumes, saVol)

	saMount := corev1.VolumeMount{
		Name: saVol.Name,
		// Default (by convention) location for mounting projected ServiceAccounts
		MountPath: "/var/run/secrets/openshift/serviceaccount",
		ReadOnly:  true,
	}
	mounts = append(mounts, saMount)

	image := os.Getenv("IMAGE")

	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
	if cr.Spec.Resources != nil {
		resources = *cr.Spec.Resources
	}

	nodes, err := coreClient.Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "topology.kubernetes.io/zone"})
	if err != nil {
		return corev1.PodTemplateSpec{}, deps, fmt.Errorf("could not check nodes for zone failure domain: %s", err)
	}
	hasZoneFailureDomain := len(nodes.Items) >= 1

	// defaults topology spread constraints to both zone, node and workers.
	// on SNO environments, these constraints will always work, since the
	// skew will always be 0.
	// some bare metal cluster nodes will not include the zone labels, in
	// which case we just omit the related constraint.
	// we constraint scheduling to workers because we want to reduce the
	// scope of scheduling to workers only. we need this constraint
	// because tainted nodes (such as control plane nodes) are not excluded
	// from skew calculations, so if we don't limit scheduling to workers
	// the maxSkew won't allow more than one pod to be scheduled per node.
	// see https://k8s.io/docs/concepts/workloads/pods/pod-topology-spread-constraints
	// and https://github.com/kubernetes/kubernetes/issues/80921 for details.
	topologySpreadConstraints := []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           1,
			TopologyKey:       "kubernetes.io/hostname",
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: defaults.DeploymentLabels,
			},
		},
		{
			MaxSkew:           1,
			TopologyKey:       "node-role.kubernetes.io/worker",
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: defaults.DeploymentLabels,
			},
		},
	}
	if hasZoneFailureDomain {
		zoneConstraint := corev1.TopologySpreadConstraint{
			MaxSkew:           1,
			TopologyKey:       "topology.kubernetes.io/zone",
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: defaults.DeploymentLabels,
			},
		}
		topologySpreadConstraints = append(topologySpreadConstraints, zoneConstraint)
	}

	// topology spread constraints might conflict with node selectors, so we
	// do not set defaults when they're specified.
	if cr.Spec.NodeSelector != nil {
		topologySpreadConstraints = nil
	}

	if cr.Spec.TopologySpreadConstraints != nil {
		topologySpreadConstraints = cr.Spec.TopologySpreadConstraints
	}

	// if user has provided an affinity through config spec we use it here, if not
	// then we fallback to a preferred affinity configuration. we only require a
	// certain affinity during schedule if the number of replicas is defined to two.
	affinity := cr.Spec.Affinity
	if affinity == nil && cr.Spec.Replicas == 2 {
		affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						TopologyKey: "kubernetes.io/hostname",
						Namespaces: []string{
							defaults.ImageRegistryOperatorNamespace,
						},
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: defaults.DeploymentLabels,
						},
					},
				},
			},
		}
	}

	nodeSelectors := map[string]string{}
	for k, v := range cr.Spec.NodeSelector {
		nodeSelectors[k] = v
	}
	if _, ok := nodeSelectors["kubernetes.io/os"]; !ok {
		nodeSelectors["kubernetes.io/os"] = "linux"
	}

	gracePeriod := int64(55)

	spec := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      defaults.DeploymentLabels,
			Annotations: defaults.DeploymentAnnotations,
		},
		Spec: corev1.PodSpec{
			Tolerations:       cr.Spec.Tolerations,
			NodeSelector:      nodeSelectors,
			PriorityClassName: "system-cluster-critical",
			Containers: []corev1.Container{
				{
					Name:  "registry",
					Image: image,
					Command: []string{
						"/bin/sh",
						"-c",
						"mkdir -p /etc/pki/ca-trust/extracted/edk2 /etc/pki/ca-trust/extracted/java /etc/pki/ca-trust/extracted/openssl /etc/pki/ca-trust/extracted/pem && update-ca-trust extract --output /etc/pki/ca-trust/extracted/ && exec /usr/bin/dockerregistry",
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(defaults.ContainerPort),
							Protocol:      "TCP",
						},
					},
					Env:            env,
					VolumeMounts:   mounts,
					LivenessProbe:  generateLivenessProbeConfig(),
					ReadinessProbe: generateReadinessProbeConfig(),
					Resources:      resources,
					// Once the pod is deleted, its endpoint should be removed
					// from routers, load balancers, and nodes. We'll give 25
					// seconds to propagate before we actually shutdown the
					// registry.
					Lifecycle: &corev1.Lifecycle{
						PreStop: &corev1.LifecycleHandler{
							Exec: &corev1.ExecAction{
								Command: []string{"sleep", "25"},
							},
						},
					},
					TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
					SecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: ptr.To(true),
					},
				},
			},
			Volumes:                       volumes,
			ServiceAccountName:            defaults.ServiceAccountName,
			SecurityContext:               securityContext,
			Affinity:                      affinity,
			TopologySpreadConstraints:     topologySpreadConstraints,
			TerminationGracePeriodSeconds: &gracePeriod,
		},
	}

	return spec, deps, nil
}
