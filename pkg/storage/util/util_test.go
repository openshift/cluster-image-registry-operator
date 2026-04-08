package util

import (
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	configv1 "github.com/openshift/api/config/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

type MockInfrastructureLister struct {
	infraName string
}

func (m MockInfrastructureLister) Get(name string) (*configv1.Infrastructure, error) {
	return &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: m.infraName,
			PlatformStatus:     &configv1.PlatformStatus{},
		},
	}, nil
}

func (m MockInfrastructureLister) List(selector labels.Selector) ([]*configv1.Infrastructure, error) {
	var list []*configv1.Infrastructure
	return list, nil
}

func TestGenerateDeterministicStorageName(t *testing.T) {
	multiDash := regexp.MustCompile(`-{2,}`)

	for _, tt := range []struct {
		name           string
		infraName      string
		additionalInfo []string
		wantName       string
	}{
		{
			name:           "When infra name and region are provided, it should produce a deterministic name",
			infraName:      "my-cluster-abc12",
			additionalInfo: []string{"us-east-1"},
			wantName:       "my-cluster-abc12-image-registry-us-east-1",
		},
		{
			name:           "When called twice with same inputs, it should produce the same name",
			infraName:      "my-cluster-abc12",
			additionalInfo: []string{"us-east-1"},
			wantName:       "my-cluster-abc12-image-registry-us-east-1",
		},
		{
			name:           "When no additional info is provided, it should use infra name and registry name only",
			infraName:      "my-cluster",
			additionalInfo: nil,
			wantName:       "my-cluster-image-registry",
		},
		{
			name:           "When infra name has double dashes, it should collapse them",
			infraName:      "my--cluster",
			additionalInfo: []string{"us-east-1"},
			wantName:       "my-cluster-image-registry-us-east-1",
		},
		{
			name:           "When infra name has upper case, it should lowercase the result",
			infraName:      "MY-CLUSTER",
			additionalInfo: []string{"us-east-1"},
			wantName:       "my-cluster-image-registry-us-east-1",
		},
		{
			name:           "When name exceeds 62 chars, it should truncate to 62",
			infraName:      "a-very-long-infrastructure-name-that-exceeds",
			additionalInfo: []string{"us-gov-west-1"},
			// "a-very-long-infrastructure-name-that-exceeds-image-registry-us" = 63 chars, truncated to 62
		},
		{
			name:           "When truncated name ends with dash, it should replace dash with s",
			infraName:      "a-very-long-infrastructure-name-that-exceeds-things",
			additionalInfo: []string{"us-gov-west-1"},
		},
		{
			name:           "When name is exactly 62 chars ending with dash, it should replace dash with s",
			infraName:      "exactly-sixty-two-chars-infra-name-ending-",
			additionalInfo: []string{"us-east-1"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := regopclient.StorageListers{Infrastructures: MockInfrastructureLister{
				infraName: tt.infraName,
			}}

			n, err := GenerateDeterministicStorageName(&l, tt.additionalInfo...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// If we have an exact expected name, verify it
			if tt.wantName != "" {
				if n != tt.wantName {
					t.Errorf("expected %q, got %q", tt.wantName, n)
				}
			}

			// Name must not exceed 62 characters
			if len(n) > 62 {
				t.Errorf("name should not exceed 62 characters, but was %d: %s", len(n), n)
			}

			// Name must be lowercase
			if n != strings.ToLower(n) {
				t.Errorf("name should be lowercase: %s", n)
			}

			// Name must not contain multiple consecutive dashes
			if multiDash.MatchString(n) {
				t.Errorf("name should not contain consecutive dashes: %s", n)
			}

			// Name must not end with a dash
			if strings.HasSuffix(n, "-") {
				t.Errorf("name should not end with a dash: %s", n)
			}

			// Calling again with the same inputs must produce the same result
			n2, err := GenerateDeterministicStorageName(&l, tt.additionalInfo...)
			if err != nil {
				t.Fatalf("unexpected error on second call: %v", err)
			}
			if n != n2 {
				t.Errorf("name should be deterministic: first=%q, second=%q", n, n2)
			}
		})
	}
}

func TestGenerateStorageName(t *testing.T) {
	multiDash := regexp.MustCompile(`-{2,}`)
	replaceMultiDash := regexp.MustCompile(`-{1,}`)
	for _, tt := range []struct {
		name           string
		infraName      string
		additionalInfo []string
	}{
		{
			name:           "nil additional info",
			infraName:      "valid-infra-name",
			additionalInfo: nil,
		},
		{
			name:           "empty slice of additional info",
			infraName:      "valid-infra-name",
			additionalInfo: []string{},
		},
		{
			name:           "slice of empty strings",
			infraName:      "valid-infra-name",
			additionalInfo: []string{"", ""},
		},
		{
			name:           "one additional",
			infraName:      "valid-infra-name",
			additionalInfo: []string{"test1"},
		},
		{
			name:           "two additionals",
			infraName:      "valid-infra-name",
			additionalInfo: []string{"test1", "test2"},
		},
		{
			name:           "really long additionals",
			infraName:      "valid-infra-name",
			additionalInfo: []string{"abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},
		},
		{
			name:           "double dashes infra name",
			infraName:      "invalid-infra--name",
			additionalInfo: []string{"test1", "test2"},
		},
		{
			name:           "invalid infra name",
			infraName:      "invalid-infra-name---",
			additionalInfo: []string{"test1", "test2"},
		},
		{
			name:           "upper case on infra name",
			infraName:      "MY-INFRA",
			additionalInfo: []string{"test1", "test2"},
		},
		{
			name:           "govcloud region name",
			infraName:      "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			additionalInfo: []string{"us-gov-west-1"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := regopclient.StorageListers{Infrastructures: MockInfrastructureLister{
				infraName: tt.infraName,
			}}

			n, err := GenerateStorageName(&l, tt.additionalInfo...)
			if err != nil {
				t.Errorf("%v", err)
			}

			rawPrefix := strings.ToLower(tt.infraName + "-" + defaults.ImageRegistryName)
			wantedPrefix := replaceMultiDash.ReplaceAllString(rawPrefix, "-")
			if len(wantedPrefix) > 62 {
				wantedPrefix = wantedPrefix[0:62]
			}

			// Name should have the wanted prefix
			if !strings.HasPrefix(n, wantedPrefix) {
				t.Errorf("name should have the prefix %s, but was %s instead", wantedPrefix, n)
			}

			// Name should be exactly 62 characters long
			if len(n) != 62 {
				t.Errorf("name should be exactly 62 characters long, but was %d instead: %s", len(n), n)
			}

			// Name should not have multiple dashes together
			if multiDash.MatchString(n) {
				t.Errorf("name should not include a double dash: %s", n)
			}

			if n != strings.ToLower(n) {
				t.Errorf("name should not contain upper case: %s", n)
			}

			if strings.HasSuffix(n, "-") {
				t.Errorf("name should not end in a dash: %s", n)
			}
		})
	}
}
