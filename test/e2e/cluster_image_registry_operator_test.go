package e2e_test

import (
	"testing"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
)

func TestClusterImageRegistryOperator(t *testing.T) {
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)

	err := ctx.InitializeClusterResources()
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}

	t.Log("Initialized cluster resources")

	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}

	t.Log("TODO", namespace)
}
