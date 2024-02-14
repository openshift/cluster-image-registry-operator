package resource

import (
	"context"
	"os"

	batchv1 "k8s.io/api/batch/v1"
	kcorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	batchset "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var _ Mutator = &generatorAzurePathFixJob{}

type generatorAzurePathFixJob struct {
	lister batchlisters.JobNamespaceLister
	client batchset.BatchV1Interface
}

func NewGeneratorAzurePathFixJob(lister batchlisters.JobNamespaceLister, client batchset.BatchV1Interface) *generatorAzurePathFixJob {
	return &generatorAzurePathFixJob{
		lister: lister,
		client: client,
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
	backoffLimit := int32(0)
	cj := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gapfj.GetName(),
			Namespace: gapfj.GetNamespace(),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: kcorev1.PodTemplateSpec{
				Spec: kcorev1.PodSpec{
					RestartPolicy:      kcorev1.RestartPolicyOnFailure,
					ServiceAccountName: defaults.ServiceAccountName,
					PriorityClassName:  "system-cluster-critical",
					Containers: []kcorev1.Container{
						{
							Image: os.Getenv("IMAGE"),
							Resources: kcorev1.ResourceRequirements{
								Requests: kcorev1.ResourceList{
									kcorev1.ResourceCPU:    resource.MustParse("100m"),
									kcorev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							TerminationMessagePolicy: kcorev1.TerminationMessageFallbackToLogsOnError,
							Name:                     gapfj.GetName(),
							Command:                  []string{"/bin/sh"},
							Args: []string{
								"-c",
								"sleep 60",
							},
						},
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
	return commonUpdate(gapfj, o, func(obj runtime.Object) (runtime.Object, error) {
		return gapfj.client.Jobs(gapfj.GetNamespace()).Update(
			context.TODO(), obj.(*batchv1.Job), metav1.UpdateOptions{},
		)
	})
}

func (gapfj *generatorAzurePathFixJob) Delete(opts metav1.DeleteOptions) error {
	return gapfj.client.Jobs(gapfj.GetNamespace()).Delete(
		context.TODO(), gapfj.GetName(), opts,
	)
}

func (gapfj *generatorAzurePathFixJob) Owned() bool {
	return true
}
