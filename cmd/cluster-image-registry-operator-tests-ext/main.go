/*
This command is used to run the Cluster Image Registry Operator tests extension for OpenShift.
It registers the cluster-image-registry-operator tests with the OpenShift Tests Extension framework
and provides a command-line interface to execute them.
For further information, please refer to the documentation at:
https://github.com/openshift-eng/openshift-tests-extension/blob/main/cmd/example-tests/main.go
*/
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	otecmd "github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	oteextension "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	oteginkgo "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	"github.com/openshift/cluster-image-registry-operator/pkg/version"

	"k8s.io/klog/v2"
)

func main() {
	cmd, err := newOperatorTestCommand()
	if err != nil {
		klog.Fatal(err)
	}

	code := cli.Run(cmd)
	os.Exit(code)
}

func newOperatorTestCommand() (*cobra.Command, error) {
	registry, err := prepareOperatorTestsRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare operator tests registry: %w", err)
	}

	cmd := &cobra.Command{
		Use:   "cluster-image-registry-operator-tests-ext",
		Short: "A binary used to run cluster-image-registry-operator tests as part of OTE.",
		Run: func(cmd *cobra.Command, args []string) {
			// no-op, logic is provided by the OTE framework
			if err := cmd.Help(); err != nil {
				klog.Fatal(err)
			}
		},
	}

	if v := version.Version; len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(otecmd.DefaultExtensionCommands(registry)...)

	return cmd, nil
}

// prepareOperatorTestsRegistry creates the OTE registry for this operator.
//
// Note:
//
// This method must be called before adding the registry to the OTE framework.
func prepareOperatorTestsRegistry() (*oteextension.Registry, error) {
	registry := oteextension.NewRegistry()
	extension := oteextension.NewExtension("openshift", "payload", "cluster-image-registry-operator")

	// The following suite runs tests that verify the image-registry operator behaviour.
	// This suite is executed only on pull requests targeting this repository.
	// Tests tagged with [Serial] are included in this suite.
	extension.AddSuite(oteextension.Suite{
		Name:        "openshift/cluster-image-registry-operator/operator/serial",
		Parallelism: 1,
		Qualifiers: []string{
			`name.contains("[Serial]")`,
		},
	})

	// The following suite runs tests that verify the image-registry operator behaviour.
	// This suite is executed only on pull requests targeting this repository.
	// Tests not tagged with [Serial] are included in this suite.
	extension.AddSuite(oteextension.Suite{
		Name:        "openshift/cluster-image-registry-operator/operator/parallel",
		Parallelism: 1,
		Qualifiers: []string{
			`!name.contains("[Serial]")`,
		},
	})

	// Build OTE specs from Ginkgo tests
	specs, err := oteginkgo.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		return nil, fmt.Errorf("couldn't build extension test specs from ginkgo: %w", err)
	}

	extension.AddSpecs(specs)
	registry.Register(extension)
	return registry, nil
}
