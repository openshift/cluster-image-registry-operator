package resource

import (
	"context"
	"fmt"
	"os"

	batchv1 "k8s.io/api/batch/v1"
	batchapi "k8s.io/api/batch/v1beta1"
	kcorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	batchset "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	batchlisters "k8s.io/client-go/listers/batch/v1beta1"

	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
	configv1listers "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	"github.com/openshift/library-go/pkg/operator/loglevel"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var (
	defaultSuspend                          = false
	defaultSchedule                         = "0 0 * * *"
	defaultStartingDeadlineSeconds    int64 = 3600
	defaultFailedJobsHistoryLimit     int32 = 3
	defaultSuccessfulJobsHistoryLimit int32 = 3
	defaultKeepTagRevisions                 = 3
	defaultKeepYoungerThan                  = "60m"
	defaultTolerations                      = []kcorev1.Toleration{}
	defaultNodeSelector                     = map[string]string{}
	defaultResources                        = kcorev1.ResourceRequirements{
		Requests: kcorev1.ResourceList{
			kcorev1.ResourceCPU:    resource.MustParse("100m"),
			kcorev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
	defaultAffinity kcorev1.Affinity
)

var _ Mutator = &generatorPrunerCronJob{}

type generatorPrunerCronJob struct {
	lister            batchlisters.CronJobNamespaceLister
	client            batchset.BatchV1beta1Interface
	prunerLister      imageregistryv1listers.ImagePrunerLister
	imageConfigLister configv1listers.ImageLister
}

func newGeneratorPrunerCronJob(lister batchlisters.CronJobNamespaceLister, client batchset.BatchV1beta1Interface, prunerLister imageregistryv1listers.ImagePrunerLister, imageConfigLister configv1listers.ImageLister) *generatorPrunerCronJob {
	return &generatorPrunerCronJob{
		lister:            lister,
		client:            client,
		prunerLister:      prunerLister,
		imageConfigLister: imageConfigLister,
	}
}

func (gcj *generatorPrunerCronJob) Type() runtime.Object {
	return &batchapi.CronJob{}
}

func (gcj *generatorPrunerCronJob) GetNamespace() string {
	return defaults.ImageRegistryOperatorNamespace
}

func (gcj *generatorPrunerCronJob) GetName() string {
	return "image-pruner"
}

func (gcj *generatorPrunerCronJob) expected() (runtime.Object, error) {
	cr, err := gcj.prunerLister.Get(defaults.ImageRegistryImagePrunerResourceName)
	if err != nil {
		return nil, err
	}

	imageConfig, err := gcj.imageConfigLister.Get("cluster")
	if err != nil {
		return nil, err
	}

	args := []string{
		"adm",
		"prune",
		"images",
		"--confirm=true",
		"--certificate-authority=/var/run/configmaps/serviceca/service-ca.crt",
		fmt.Sprintf("--keep-tag-revisions=%d", gcj.getKeepTagRevisions(cr)),
		fmt.Sprintf("--keep-younger-than=%s", gcj.getKeepYoungerThan(cr)),
		fmt.Sprintf("--ignore-invalid-refs=%t", cr.Spec.IgnoreInvalidImageReferences),
		fmt.Sprintf("--loglevel=%d", gcj.getLogLevel(cr)),
	}

	if imageConfig.Status.InternalRegistryHostname != "" {
		args = append(args,
			"--prune-registry=true",
			fmt.Sprintf("--registry-url=https://%s", imageConfig.Status.InternalRegistryHostname),
		)
	} else {
		args = append(args, "--prune-registry=false")
	}

	backoffLimit := int32(0)
	cj := &batchapi.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gcj.GetName(),
			Namespace: gcj.GetNamespace(),
		},
		Spec: batchapi.CronJobSpec{
			Suspend:                    gcj.getSuspend(cr),
			Schedule:                   gcj.getSchedule(cr),
			ConcurrencyPolicy:          batchapi.ForbidConcurrent,
			FailedJobsHistoryLimit:     gcj.getFailedJobsHistoryLimit(cr),
			SuccessfulJobsHistoryLimit: gcj.getSuccessfulJobsHistoryLimit(cr),
			StartingDeadlineSeconds:    &defaultStartingDeadlineSeconds,
			JobTemplate: batchapi.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: &backoffLimit,
					Template: kcorev1.PodTemplateSpec{
						Spec: kcorev1.PodSpec{
							RestartPolicy:      kcorev1.RestartPolicyNever,
							ServiceAccountName: "pruner",
							Affinity:           gcj.getAffinity(cr),
							NodeSelector:       gcj.getNodeSelector(cr),
							Tolerations:        gcj.getTolerations(cr),
							Volumes: []kcorev1.Volume{
								{
									Name: "serviceca",
									VolumeSource: kcorev1.VolumeSource{
										ConfigMap: &kcorev1.ConfigMapVolumeSource{
											LocalObjectReference: kcorev1.LocalObjectReference{
												Name: "serviceca",
											},
										},
									},
								},
							},
							Containers: []kcorev1.Container{
								{
									Image:                    os.Getenv("IMAGE_PRUNER"),
									Resources:                gcj.getResourceRequirements(cr),
									TerminationMessagePolicy: kcorev1.TerminationMessageFallbackToLogsOnError,
									Name:                     gcj.GetName(),
									Command:                  []string{"oc"},
									Args:                     args,
									VolumeMounts: []kcorev1.VolumeMount{
										{
											Name:      "serviceca",
											MountPath: "/var/run/configmaps/serviceca",
											ReadOnly:  true,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	cj.Spec.JobTemplate.Labels = map[string]string{"created-by": gcj.GetName()}
	return cj, nil
}

func (gcj *generatorPrunerCronJob) getSuspend(cr *imageregistryapiv1.ImagePruner) *bool {
	if cr.Spec.Suspend != nil {
		return cr.Spec.Suspend
	}
	return &defaultSuspend
}

func (gcj *generatorPrunerCronJob) getSchedule(cr *imageregistryapiv1.ImagePruner) string {
	if len(cr.Spec.Schedule) != 0 {
		return cr.Spec.Schedule
	}
	return defaultSchedule
}

func (gcj *generatorPrunerCronJob) getAffinity(cr *imageregistryapiv1.ImagePruner) *kcorev1.Affinity {
	if cr.Spec.Affinity != nil {
		return cr.Spec.Affinity
	}
	return &defaultAffinity
}

func (gcj *generatorPrunerCronJob) getNodeSelector(cr *imageregistryapiv1.ImagePruner) map[string]string {
	if cr.Spec.NodeSelector != nil {
		return cr.Spec.NodeSelector
	}
	return defaultNodeSelector
}

func (gcj *generatorPrunerCronJob) getTolerations(cr *imageregistryapiv1.ImagePruner) []kcorev1.Toleration {
	if cr.Spec.NodeSelector != nil {
		return cr.Spec.Tolerations
	}
	return defaultTolerations
}

func (gcj *generatorPrunerCronJob) getResourceRequirements(cr *imageregistryapiv1.ImagePruner) kcorev1.ResourceRequirements {
	if cr.Spec.Resources != nil {
		return *cr.Spec.Resources
	}
	return defaultResources
}

func (gcj *generatorPrunerCronJob) getFailedJobsHistoryLimit(cr *imageregistryapiv1.ImagePruner) *int32 {
	if cr.Spec.FailedJobsHistoryLimit != nil {
		return cr.Spec.FailedJobsHistoryLimit
	}
	return &defaultFailedJobsHistoryLimit
}

func (gcj *generatorPrunerCronJob) getSuccessfulJobsHistoryLimit(cr *imageregistryapiv1.ImagePruner) *int32 {
	if cr.Spec.SuccessfulJobsHistoryLimit != nil {
		return cr.Spec.SuccessfulJobsHistoryLimit
	}
	return &defaultSuccessfulJobsHistoryLimit
}

func (gcj *generatorPrunerCronJob) getKeepTagRevisions(cr *imageregistryapiv1.ImagePruner) int {
	if cr.Spec.KeepTagRevisions != nil {
		return *cr.Spec.KeepTagRevisions
	}
	return defaultKeepTagRevisions
}

func (gcj *generatorPrunerCronJob) getKeepYoungerThan(cr *imageregistryapiv1.ImagePruner) string {
	if cr.Spec.KeepYoungerThanDuration != nil {
		return cr.Spec.KeepYoungerThanDuration.Duration.String()
	}
	if cr.Spec.KeepYoungerThan != nil {
		return cr.Spec.KeepYoungerThan.String()
	}
	return defaultKeepYoungerThan
}

func (gcj *generatorPrunerCronJob) getLogLevel(cr *imageregistryapiv1.ImagePruner) int {
	level := loglevel.LogLevelToVerbosity(cr.Spec.LogLevel)
	if level == 2 {
		// The Normal log level is 2, but it causes `oc` to print stack traces
		// for all goroutines in case of an error. It makes termination
		// messages meaningless as they contain only few last lines of the log.
		// The default value for `oc` is 0 (i.e. commands are usually run
		// without -v), but let's pick a value closer to 2 that doesn't cause
		// problems.
		return 1
	}
	return level
}

func (gcj *generatorPrunerCronJob) Get() (runtime.Object, error) {
	return gcj.lister.Get(gcj.GetName())
}

func (gcj *generatorPrunerCronJob) Create() (runtime.Object, error) {
	return commonCreate(gcj, func(obj runtime.Object) (runtime.Object, error) {
		return gcj.client.CronJobs(gcj.GetNamespace()).Create(
			context.TODO(), obj.(*batchapi.CronJob), metav1.CreateOptions{},
		)
	})
}

func (gcj *generatorPrunerCronJob) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gcj, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcj.client.CronJobs(gcj.GetNamespace()).Update(
			context.TODO(), obj.(*batchapi.CronJob), metav1.UpdateOptions{},
		)
	})
}

func (gcj *generatorPrunerCronJob) Delete(opts metav1.DeleteOptions) error {
	return gcj.client.CronJobs(gcj.GetNamespace()).Delete(
		context.TODO(), gcj.GetName(), opts,
	)
}

func (gcj *generatorPrunerCronJob) Owned() bool {
	return true
}
