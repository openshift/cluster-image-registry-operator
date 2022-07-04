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
		})
	}
}
