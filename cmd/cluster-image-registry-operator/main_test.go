package main

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
)

func Test_readAndParseControllerConfig(t *testing.T) {
	testCases := []struct {
		name          string
		configContent string
		expected      *configv1.GenericControllerConfig
		expectError   bool
	}{
		{
			name: "valid config with bind address and TLS version",
			configContent: `apiVersion: config.openshift.io/v1
kind: GenericControllerConfig
servingInfo:
  bindAddress: "0.0.0.0:8443"
  minTLSVersion: VersionTLS12
`,
			expected: &configv1.GenericControllerConfig{
				ServingInfo: configv1.HTTPServingInfo{
					ServingInfo: configv1.ServingInfo{
						BindAddress:   "0.0.0.0:8443",
						MinTLSVersion: "VersionTLS12",
					},
				},
			},
			expectError: false,
		},
		{
			name:          "invalid YAML returns error",
			configContent: `this is not valid yaml: [[[`,
			expectError:   true,
		},
		{
			name: "partial config with only bind address",
			configContent: `apiVersion: config.openshift.io/v1
kind: GenericControllerConfig
servingInfo:
  bindAddress: "localhost:9090"
`,
			expected: &configv1.GenericControllerConfig{
				ServingInfo: configv1.HTTPServingInfo{
					ServingInfo: configv1.ServingInfo{
						BindAddress: "localhost:9090",
					},
				},
			},
			expectError: false,
		},
		{
			name: "config with cipher suites",
			configContent: `apiVersion: config.openshift.io/v1
kind: GenericControllerConfig
servingInfo:
  bindAddress: "0.0.0.0:8443"
  minTLSVersion: VersionTLS13
  cipherSuites:
  - TLS_AES_128_GCM_SHA256
  - TLS_AES_256_GCM_SHA384
`,
			expected: &configv1.GenericControllerConfig{
				ServingInfo: configv1.HTTPServingInfo{
					ServingInfo: configv1.ServingInfo{
						BindAddress:   "0.0.0.0:8443",
						MinTLSVersion: "VersionTLS13",
						CipherSuites: []string{
							"TLS_AES_128_GCM_SHA256",
							"TLS_AES_256_GCM_SHA384",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "config with multiple TLS settings",
			configContent: `apiVersion: config.openshift.io/v1
kind: GenericControllerConfig
servingInfo:
  bindAddress: "127.0.0.1:6443"
  minTLSVersion: VersionTLS13
  cipherSuites:
  - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
`,
			expected: &configv1.GenericControllerConfig{
				ServingInfo: configv1.HTTPServingInfo{
					ServingInfo: configv1.ServingInfo{
						BindAddress:   "127.0.0.1:6443",
						MinTLSVersion: "VersionTLS13",
						CipherSuites: []string{
							"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "empty bind address",
			configContent: `apiVersion: config.openshift.io/v1
kind: GenericControllerConfig
servingInfo:
  bindAddress: ""
  minTLSVersion: VersionTLS12
`,
			expected: &configv1.GenericControllerConfig{
				ServingInfo: configv1.HTTPServingInfo{
					ServingInfo: configv1.ServingInfo{
						BindAddress:   ":60000",
						MinTLSVersion: "VersionTLS12",
					},
				},
			},
			expectError: false,
		},
		{
			name:          "no config path",
			configContent: "",
			expected: &configv1.GenericControllerConfig{
				ServingInfo: configv1.HTTPServingInfo{
					ServingInfo: configv1.ServingInfo{
						BindAddress: ":60000",
					},
				},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var configPath string
			if tc.configContent == "" {
				configPath = ""
			} else {
				tmpFile, err := os.CreateTemp("", "config-*.yaml")
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				defer os.Remove(tmpFile.Name())

				if _, err := tmpFile.Write([]byte(tc.configContent)); err != nil {
					t.Fatalf("failed to write temp file: %v", err)
				}
				tmpFile.Close()
				configPath = tmpFile.Name()
			}

			config, err := readAndParseControllerConfig(configPath)

			if tc.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config == nil {
				t.Fatal("expected config, got nil")
			}

			if diff := cmp.Diff(tc.expected.ServingInfo, config.ServingInfo); diff != "" {
				t.Errorf("ServingInfo mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_readAndParseControllerConfig_nonExistentFile(t *testing.T) {
	_, err := readAndParseControllerConfig("/nonexistent/file.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}
