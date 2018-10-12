package util

import (
	"errors"
	"testing"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	imageapiv1 "github.com/openshift/api/image/v1"
)

const (
	GroupName          = "image.openshift.io"
	APIVersionInternal = "__internal"
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: APIVersionInternal}
)

// Kind takes an unqualified kind and returns back a Group qualified GroupKind
func Kind(kind string) schema.GroupKind {
	return SchemeGroupVersion.WithKind(kind).GroupKind()
}

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// TestIsErrorQuotaExceeded verifies that if a resource exceedes allowed usage, the admission will return
// error we can recognize.
func TestIsErrorQuotaExceeded(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		shouldMatch bool
	}{
		{
			name: "unrelated error",
			err:  errors.New("unrelated"),
		},
		{
			name: "wrong type",
			err:  errors.New(errQuotaMessageString),
		},
		{
			name: "wrong kapi type",
			err:  kerrors.NewUnauthorized(errQuotaMessageString),
		},
		{
			name: "unrelated forbidden error",
			err:  kerrors.NewForbidden(imageapiv1.Resource("imageStreams"), "is", errors.New("unrelated")),
		},
		{
			name: "unrelated invalid error",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Required(field.NewPath("imageStream").Child("Spec"), "detail"),
				}),
		},
		{
			name: "quota error not recognized with invalid reason",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Forbidden(field.NewPath("imageStreams"), errQuotaMessageString),
				}),
		},
		{
			name: "quota unknown error not recognized with invalid reason",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Forbidden(field.NewPath("imageStreams"), errQuotaUnknownMessageString),
				}),
		},
		{
			name:        "quota exceeded error",
			err:         kerrors.NewForbidden(imageapiv1.Resource("imageStream"), "is", errors.New(errQuotaMessageString)),
			shouldMatch: true,
		},
		{
			name:        "quota unknown error",
			err:         kerrors.NewForbidden(imageapiv1.Resource("imageStream"), "is", errors.New(errQuotaUnknownMessageString)),
			shouldMatch: true,
		},
		{
			name:        "limits exceeded error with forbidden reason",
			err:         kerrors.NewForbidden(imageapiv1.Resource("imageStream"), "is", errors.New(errLimitsMessageString)),
			shouldMatch: true,
		},
		{
			name: "limits exceeded error with invalid reason",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Forbidden(field.NewPath("imageStream"), errLimitsMessageString),
				}),
			shouldMatch: true,
		},
	} {
		match := IsErrorQuotaExceeded(tc.err)

		if !match && tc.shouldMatch {
			t.Errorf("[%s] expected to match error [%T]: %v", tc.name, tc.err, tc.err)
		}
		if match && !tc.shouldMatch {
			t.Errorf("[%s] expected not to match error [%T]: %v", tc.name, tc.err, tc.err)
		}
	}
}

// TestIsErrorLimitExceeded tests for limit errors.
func TestIsErrorLimitExceeded(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		shouldMatch bool
	}{
		{
			name: "unrelated error",
			err:  errors.New("unrelated"),
		},
		{
			name: "wrong type",
			err:  errors.New(errQuotaMessageString),
		},
		{
			name: "wrong kapi type",
			err:  kerrors.NewUnauthorized(errQuotaMessageString),
		},
		{
			name: "unrelated forbidden error",
			err:  kerrors.NewForbidden(imageapiv1.Resource("imageStreams"), "is", errors.New("unrelated")),
		},
		{
			name: "unrelated invalid error",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Required(field.NewPath("imageStream").Child("Spec"), "detail"),
				}),
		},
		{
			name: "quota error not recognized with invalid reason",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Forbidden(field.NewPath("imageStreams"), errQuotaMessageString),
				}),
		},
		{
			name: "quota unknown error not recognized with invalid reason",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Forbidden(field.NewPath("imageStreams"), errQuotaUnknownMessageString),
				}),
		},
		{
			name: "quota exceeded error",
			err:  kerrors.NewForbidden(imageapiv1.Resource("imageStream"), "is", errors.New(errQuotaMessageString)),
		},
		{
			name: "quota unknown error",
			err:  kerrors.NewForbidden(imageapiv1.Resource("imageStream"), "is", errors.New(errQuotaUnknownMessageString)),
		},
		{
			name:        "limits exceeded error with forbidden reason",
			err:         kerrors.NewForbidden(imageapiv1.Resource("imageStream"), "is", errors.New(errLimitsMessageString)),
			shouldMatch: true,
		},
		{
			name: "limits exceeded error with invalid reason",
			err: kerrors.NewInvalid(Kind("imageStreams"), "is",
				field.ErrorList{
					field.Forbidden(field.NewPath("imageStream"), errLimitsMessageString),
				}),
			shouldMatch: true,
		},
	} {
		match := IsErrorLimitExceeded(tc.err)

		if !match && tc.shouldMatch {
			t.Errorf("[%s] expected to match error [%T]: %v", tc.name, tc.err, tc.err)
		}
		if match && !tc.shouldMatch {
			t.Errorf("[%s] expected not to match error [%T]: %v", tc.name, tc.err, tc.err)
		}
	}
}
