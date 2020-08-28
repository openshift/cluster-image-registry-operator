package pvc

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func TestStorageManagementState(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		expectedManagementState string
		config                  *imageregistryv1.Config
		objects                 []runtime.Object
		err                     string
	}{
		{
			name:                    "empty without pvc in place",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{},
					},
				},
			},
		},
		{
			name:                    "empty without pvc in place (management state set)",
			expectedManagementState: "foo",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						PVC:             &imageregistryv1.ImageRegistryConfigStoragePVC{},
					},
				},
			},
		},
		{
			name:                    "empty with pvc in place",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{},
					},
				},
			},
			objects: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-image-registry",
						Name:      defaults.PVCImageRegistryName,
						Annotations: map[string]string{
							PVCOwnerAnnotation: "true",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteMany,
						},
					},
				},
			},
		},
		{
			name:                    "empty with pvc in place (management state set)",
			expectedManagementState: "foobar",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foobar",
						PVC:             &imageregistryv1.ImageRegistryConfigStoragePVC{},
					},
				},
			},
			objects: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-image-registry",
						Name:      defaults.PVCImageRegistryName,
						Annotations: map[string]string{
							PVCOwnerAnnotation: "true",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteMany,
						},
					},
				},
			},
		},
		{
			name: "empty with pvc in place (not owned)",
			err:  "it already exists and is not owned by the operator",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{},
					},
				},
			},
			objects: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-image-registry",
						Name:      defaults.PVCImageRegistryName,
					},
				},
			},
		},
		{
			name:                    "empty with pvc in place (not owned - management state set)",
			err:                     "it already exists and is not owned by the operator",
			expectedManagementState: "foo",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						PVC:             &imageregistryv1.ImageRegistryConfigStoragePVC{},
					},
				},
			},
			objects: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-image-registry",
						Name:      defaults.PVCImageRegistryName,
					},
				},
			},
		},
		{
			name: "user custom pvc (wrong access mode)",
			err:  "does not contain the necessary access modes",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{
							Claim: "user-provided-pvc",
						},
					},
				},
			},
			objects: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-image-registry",
						Name:      "user-provided-pvc",
					},
				},
			},
		},
		{
			name:                    "user custom pvc (wrong access mode - management state set)",
			err:                     "does not contain the necessary access modes",
			expectedManagementState: "bar",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "bar",
						PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{
							Claim: "user-provided-pvc",
						},
					},
				},
			},
			objects: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-image-registry",
						Name:      "user-provided-pvc",
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cliset := fake.NewSimpleClientset()
			if len(tt.objects) > 0 {
				cliset = fake.NewSimpleClientset(tt.objects...)
			}

			drv := &driver{
				Namespace: "openshift-image-registry",
				Config:    tt.config.Spec.Storage.PVC,
				Client:    cliset.CoreV1(),
			}

			if err := drv.CreateStorage(tt.config); err != nil {
				if len(tt.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf(
						"expected error to be %q, %v received instead",
						tt.err,
						err,
					)
				}
			}

			if tt.config.Spec.Storage.ManagementState != tt.expectedManagementState {
				t.Errorf(
					"expecting state to be %q, %q instead",
					tt.expectedManagementState,
					tt.config.Spec.Storage.ManagementState,
				)
			}
		})
	}
}
