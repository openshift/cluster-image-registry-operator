package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestBootstrapFailToUpdateSpec(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	logs, err := framework.GetOperatorLogs(context.Background(), te.Client())
	if err != nil {
		t.Fatalf("error reading operator logs: %s", err)
	}

	for _, podLogs := range logs {
		for _, containerLogs := range podLogs {
			for _, logLine := range containerLogs {
				if strings.Contains(logLine, "unable to update config spec") {
					t.Errorf("error on spec update found, this should not happen")
				}
			}
		}
	}
}
