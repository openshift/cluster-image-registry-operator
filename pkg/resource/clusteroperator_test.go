package resource

import (
	"os"
	"reflect"
	"testing"

	imregv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"

	cfgapi "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	kerror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
)

type deployLister struct {
	deploys   map[string]appsv1.Deployment
	failOnGet bool
}

func (d deployLister) Get(name string) (*appsv1.Deployment, error) {
	if d.failOnGet {
		return nil, &kerror.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonInternalError,
			},
		}
	}

	deploy, ok := d.deploys[name]
	if !ok {
		return nil, &kerror.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonNotFound,
			},
		}
	}
	return &deploy, nil
}

func (d deployLister) List(selector labels.Selector) ([]*appsv1.Deployment, error) {
	return nil, nil
}

func TestSyncVersions(t *testing.T) {
	lister := deployLister{}
	co := &cfgapi.ClusterOperator{}

	for _, tt := range []struct {
		name         string
		environ      map[string]string
		deploys      map[string]appsv1.Deployment
		versions     []cfgapi.OperandVersion
		modified     bool
		expectsError bool
		failOnGet    bool
		config       *imregv1.Config
	}{
		{
			name:         "no config",
			modified:     false,
			expectsError: true,
		},
		{
			name:         "no env and no deployment",
			modified:     false,
			expectsError: false,
			config:       &imregv1.Config{},
		},
		{
			name:         "an api error",
			modified:     false,
			expectsError: true,
			failOnGet:    true,
			config: &imregv1.Config{
				Spec: imregv1.ImageRegistrySpec{
					ManagementState: operatorapi.Managed,
				},
			},
		},
		{
			name:         "managed with no deployment",
			modified:     false,
			expectsError: false,
			config: &imregv1.Config{
				Spec: imregv1.ImageRegistrySpec{
					ManagementState: operatorapi.Managed,
				},
			},
		},
		{
			name:         "managed with a not ready yet deployment",
			modified:     false,
			expectsError: false,
			deploys: map[string]appsv1.Deployment{
				defaults.ImageRegistryName: {
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							defaults.VersionAnnotation: "1",
						},
					},
				},
			},
			config: &imregv1.Config{
				Spec: imregv1.ImageRegistrySpec{
					ManagementState: operatorapi.Managed,
				},
			},
		},
		{
			name:         "deployment rollout succeeded",
			modified:     true,
			expectsError: false,
			environ: map[string]string{
				"RELEASE_VERSION": "2",
			},
			deploys: map[string]appsv1.Deployment{
				defaults.ImageRegistryName: {
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							defaults.VersionAnnotation: "1",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
					},
				},
			},
			versions: []cfgapi.OperandVersion{
				{
					Name:    "operator",
					Version: "1",
				},
			},
			config: &imregv1.Config{
				Spec: imregv1.ImageRegistrySpec{
					ManagementState: operatorapi.Managed,
				},
			},
		},
		{
			name:         "update to the same version",
			modified:     false,
			expectsError: false,
			environ: map[string]string{
				"RELEASE_VERSION": "2",
			},
			deploys: map[string]appsv1.Deployment{
				defaults.ImageRegistryName: {
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							defaults.VersionAnnotation: "1",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
					},
				},
			},
			versions: []cfgapi.OperandVersion{
				{
					Name:    "operator",
					Version: "1",
				},
			},
			config: &imregv1.Config{
				Spec: imregv1.ImageRegistrySpec{
					ManagementState: operatorapi.Managed,
				},
			},
		},
		{
			name:         "removed management state",
			modified:     true,
			expectsError: false,
			environ: map[string]string{
				"RELEASE_VERSION": "3",
			},
			deploys: map[string]appsv1.Deployment{
				defaults.ImageRegistryName: {
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							defaults.VersionAnnotation: "1",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
					},
				},
			},
			versions: []cfgapi.OperandVersion{
				{
					Name:    "operator",
					Version: "3",
				},
			},
			config: &imregv1.Config{
				Spec: imregv1.ImageRegistrySpec{
					ManagementState: operatorapi.Removed,
				},
			},
		},
		{
			name:         "unmanaged management state",
			modified:     true,
			expectsError: false,
			environ: map[string]string{
				"RELEASE_VERSION": "4",
			},
			deploys: map[string]appsv1.Deployment{
				defaults.ImageRegistryName: {
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							defaults.VersionAnnotation: "1",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
					},
				},
			},
			versions: []cfgapi.OperandVersion{
				{
					Name:    "operator",
					Version: "4",
				},
			},
			config: &imregv1.Config{
				Spec: imregv1.ImageRegistrySpec{
					ManagementState: operatorapi.Unmanaged,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				for name := range tt.environ {
					os.Unsetenv(name)
				}
			}()

			for name, val := range tt.environ {
				os.Setenv(name, val)
			}

			lister.deploys, lister.failOnGet = tt.deploys, tt.failOnGet
			gen := newGeneratorClusterOperator(
				lister, nil, nil, tt.config, nil,
			)

			modified, err := gen.syncVersions(co)
			if err == nil && tt.expectsError {
				t.Errorf("expecting error, nil received")
				return
			} else if err != nil && !tt.expectsError {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if modified != tt.modified {
				t.Errorf(
					"expected modified %v, received %v instead",
					tt.modified, modified,
				)
				return
			}

			if !reflect.DeepEqual(co.Status.Versions, tt.versions) {
				t.Errorf(
					"versions mismatch, expected: %v, received: %v",
					tt.versions, co.Status.Versions,
				)
			}
		})
	}
}
