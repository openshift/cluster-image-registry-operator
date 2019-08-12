package util

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
)

var (
	infrastructureName = "jsmith-xhv4"
	prefix             = fmt.Sprintf("%s-%s", infrastructureName, imageregistryv1.ImageRegistryName)
)

type MockInfrastructureLister struct {
}

func (m MockInfrastructureLister) Get(name string) (*configv1.Infrastructure, error) {
	return &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: infrastructureName,
			PlatformStatus:     &configv1.PlatformStatus{},
		},
	}, nil
}

func (m MockInfrastructureLister) List(selector labels.Selector) ([]*configv1.Infrastructure, error) {
	var list []*configv1.Infrastructure
	return list, nil
}

func TestGenerateStorageName(t *testing.T) {
	l := regopclient.Listers{Infrastructures: MockInfrastructureLister{}}
	tests := [][]string{
		// test with nil
		nil,
		// test with no additionals
		{},
		// test with two empty strings
		{"", ""},
		// test one additional
		{"test1"},
		// test two additionals
		{"test1", "test2"},
		// test really long additionals
		{"abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},
	}

	multiDash := regexp.MustCompile(`-{2,}`)
	replaceMultiDash := regexp.MustCompile(`-{1,}`)
	for _, test := range tests {
		n, err := GenerateStorageName(&l, test...)
		if err != nil {
			t.Errorf("%v", err)
		}
		wantedPrefix := replaceMultiDash.ReplaceAllString(fmt.Sprintf("%s-%s", prefix, strings.Join(test, "-")), "-")
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
	}
}
