package resource

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	batchapi "k8s.io/api/batch/v1beta1"
	kcorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	batchset "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	batchlisters "k8s.io/client-go/listers/batch/v1beta1"

	pruningapiv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/pruner/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

const (
	cronJobOwnerAnnotation = "imageregistry.openshift.io"
)

var (
	defaultSuspend                          = false
	defaultSchedule                         = "*/1 * * * *"
	defaultStartingDeadlineSeconds    int64 = 60
	defaultFailedJobsHistoryLimit     int32 = 3
	defaultSuccessfulJobsHistoryLimit int32 = 3
	defaultKeepTagRevisions                 = 3
	defaultKeepYoungerThan                  = "60m"
	defaultResources                        = kcorev1.ResourceRequirements{
		Requests: kcorev1.ResourceList{
			kcorev1.ResourceCPU:    resource.MustParse("1"),
			kcorev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
)

// CronJobIsCreatedByOperator returns whether or not the CronJob was created by the Operator
func CronJobIsCreatedByOperator(cronjob *batchapi.CronJob) bool {
	_, ok := cronjob.Annotations[cronJobOwnerAnnotation]
	return ok
}

var _ Mutator = &generatorCronJob{}

type generatorCronJob struct {
	lister    batchlisters.CronJobNamespaceLister
	client    batchset.BatchV1beta1Interface
	namespace string
	cr        *pruningapiv1.Config
}

func newGeneratorCronJob(lister batchlisters.CronJobNamespaceLister, client batchset.BatchV1beta1Interface, params *parameters.Globals, cr *pruningapiv1.Config) *generatorCronJob {
	return &generatorCronJob{
		lister: lister,
		client: client,
		cr:     cr,
	}
}

func (gcj *generatorCronJob) Type() runtime.Object {
	return &batchapi.CronJob{}
}

func (gcj *generatorCronJob) GetGroup() string {
	return batchapi.GroupName
}

func (gcj *generatorCronJob) GetResource() string {
	return "batches"
}

func (gcj *generatorCronJob) GetNamespace() string {
	return gcj.namespace
}

func (gcj *generatorCronJob) GetName() string {
	return "image-pruner"
}

func (gcj *generatorCronJob) expected() (runtime.Object, error) {
	cj := &batchapi.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gcj.GetName(),
			Namespace:   gcj.GetNamespace(),
			Annotations: map[string]string{cronJobOwnerAnnotation: "true"},
		},
		Spec: batchapi.CronJobSpec{
			Suspend:                    gcj.getSuspend(),
			Schedule:                   gcj.getSchedule(),
			ConcurrencyPolicy:          batchapi.ForbidConcurrent,
			FailedJobsHistoryLimit:     gcj.getFailedJobsHistoryLimit(),
			SuccessfulJobsHistoryLimit: gcj.getSuccessfulJobsHistoryLimit(),
			StartingDeadlineSeconds:    gcj.getStartingDeadlineSeconds(),
			JobTemplate: batchapi.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: kcorev1.PodTemplateSpec{
						Spec: kcorev1.PodSpec{
							RestartPolicy:      kcorev1.RestartPolicyOnFailure,
							ServiceAccountName: "pruner",
							Containers: []kcorev1.Container{
								{
									Image:                    "quay.io/openshift/origin-cli:4.1",
									Resources:                gcj.getResources(),
									TerminationMessagePolicy: kcorev1.TerminationMessageFallbackToLogsOnError,
									Name:                     gcj.GetName(),
									Command:                  []string{"oc"},
									Args: []string{
										"adm",
										"prune",
										"images",
										"--certificate-authority=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.cr",
										fmt.Sprintf("--keep-tag-revision=%d", gcj.getKeepTagRevisions()),
										fmt.Sprintf("--keep-younger-than=%s", gcj.getKeepYoungerThan()),
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

	return cj, nil
}

func (gcj *generatorCronJob) getSuspend() *bool {
	if gcj.cr.Spec.Suspend != nil {
		return gcj.cr.Spec.Suspend
	}
	return &defaultSuspend
}

func (gcj *generatorCronJob) getSchedule() string {
	if len(gcj.cr.Spec.Schedule) != 0 {
		return gcj.cr.Spec.Schedule
	}
	return defaultSchedule
}

func (gcj *generatorCronJob) getStartingDeadlineSeconds() *int64 {
	if gcj.cr.Spec.StartingDeadlineSeconds != nil {
		return gcj.cr.Spec.StartingDeadlineSeconds
	}
	return &defaultStartingDeadlineSeconds
}

func (gcj *generatorCronJob) getResources() kcorev1.ResourceRequirements {
	if gcj.cr.Spec.Resources != nil {
		return *gcj.cr.Spec.Resources
	}
	return defaultResources
}

func (gcj *generatorCronJob) getFailedJobsHistoryLimit() *int32 {
	if gcj.cr.Spec.History.FailedJobsHistoryLimit != nil {
		return gcj.cr.Spec.History.FailedJobsHistoryLimit
	}
	return &defaultFailedJobsHistoryLimit
}

func (gcj *generatorCronJob) getSuccessfulJobsHistoryLimit() *int32 {
	if gcj.cr.Spec.History.SuccessfulJobsHistoryLimit != nil {
		return gcj.cr.Spec.History.SuccessfulJobsHistoryLimit
	}
	return &defaultSuccessfulJobsHistoryLimit
}

func (gcj *generatorCronJob) getKeepTagRevisions() int {
	if gcj.cr.Spec.KeepTagRevisions != nil {
		return *gcj.cr.Spec.KeepTagRevisions
	}
	return defaultKeepTagRevisions
}

func (gcj *generatorCronJob) getKeepYoungerThan() string {
	if len(gcj.cr.Spec.KeepYoungerThan) != 0 {
		return gcj.cr.Spec.KeepYoungerThan
	}
	return defaultKeepYoungerThan
}

func (gcj *generatorCronJob) Get() (runtime.Object, error) {
	return gcj.lister.Get(gcj.GetName())
}

func (gcj *generatorCronJob) Create() (runtime.Object, error) {
	return commonCreate(gcj, func(obj runtime.Object) (runtime.Object, error) {
		return gcj.client.CronJobs(gcj.GetNamespace()).Create(obj.(*batchapi.CronJob))
	})
}

func (gcj *generatorCronJob) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gcj, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcj.client.CronJobs(gcj.GetNamespace()).Update(obj.(*batchapi.CronJob))
	})
}

func (gcj *generatorCronJob) Delete(opts *metav1.DeleteOptions) error {
	return gcj.client.CronJobs(gcj.GetNamespace()).Delete(gcj.GetName(), opts)
}

func (gcj *generatorCronJob) Owned() bool {
	return true
}
