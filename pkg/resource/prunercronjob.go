package resource

import (
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
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var (
	defaultSuspend                          = false
	defaultSchedule                         = "0 0 * * *"
	defaultStartingDeadlineSeconds    int64 = 60
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
	lister       batchlisters.CronJobNamespaceLister
	client       batchset.BatchV1beta1Interface
	prunerLister imageregistryv1listers.ImagePrunerLister
	cr           *imageregistryapiv1.ImagePruner
}

func newGeneratorPrunerCronJob(lister batchlisters.CronJobNamespaceLister, client batchset.BatchV1beta1Interface, prunerLister imageregistryv1listers.ImagePrunerLister, params *parameters.Globals) *generatorPrunerCronJob {
	return &generatorPrunerCronJob{
		lister:       lister,
		client:       client,
		prunerLister: prunerLister,
	}
}

func (gcj *generatorPrunerCronJob) Type() runtime.Object {
	return &batchapi.CronJob{}
}

func (gcj *generatorPrunerCronJob) GetGroup() string {
	return batchapi.GroupName
}

func (gcj *generatorPrunerCronJob) GetResource() string {
	return "batches"
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
					Template: kcorev1.PodTemplateSpec{
						Spec: kcorev1.PodSpec{
							RestartPolicy:      kcorev1.RestartPolicyOnFailure,
							ServiceAccountName: "pruner",
							Affinity:           gcj.getAffinity(cr),
							NodeSelector:       gcj.getNodeSelector(cr),
							Tolerations:        gcj.getTolerations(cr),
							Containers: []kcorev1.Container{
								{
									Image:                    os.Getenv("IMAGE_PRUNER"),
									Resources:                gcj.getResourceRequirements(cr),
									TerminationMessagePolicy: kcorev1.TerminationMessageFallbackToLogsOnError,
									Name:                     gcj.GetName(),
									Command:                  []string{"oc"},
									Args: []string{
										"adm",
										"prune",
										"images",
										"--certificate-authority=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt",
										fmt.Sprintf("--keep-tag-revisions=%d", gcj.getKeepTagRevisions(cr)),
										fmt.Sprintf("--keep-younger-than=%s", gcj.getKeepYoungerThan(cr)),
										"--confirm=true",
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
	if cr.Spec.KeepYoungerThan != nil {
		return fmt.Sprintf("%s", cr.Spec.KeepYoungerThan)
	}
	return defaultKeepYoungerThan
}

func (gcj *generatorPrunerCronJob) Get() (runtime.Object, error) {
	return gcj.lister.Get(gcj.GetName())
}

func (gcj *generatorPrunerCronJob) Create() (runtime.Object, error) {
	return commonCreate(gcj, func(obj runtime.Object) (runtime.Object, error) {

		return gcj.client.CronJobs(gcj.GetNamespace()).Create(obj.(*batchapi.CronJob))
	})
}

func (gcj *generatorPrunerCronJob) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gcj, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcj.client.CronJobs(gcj.GetNamespace()).Update(obj.(*batchapi.CronJob))
	})
}

func (gcj *generatorPrunerCronJob) Delete(opts *metav1.DeleteOptions) error {
	return gcj.client.CronJobs(gcj.GetNamespace()).Delete(gcj.GetName(), opts)
}

func (gcj *generatorPrunerCronJob) Owned() bool {
	return true
}
