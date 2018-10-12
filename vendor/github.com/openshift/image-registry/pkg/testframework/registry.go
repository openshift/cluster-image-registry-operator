package testframework

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/context"

	"github.com/openshift/image-registry/pkg/cmd/dockerregistry"
	registryconfig "github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	"github.com/openshift/image-registry/pkg/testutil"
)

type CloseFunc func() error

type RegistryOption interface {
	Apply(dockerConfig *configuration.Configuration, extraConfig *registryconfig.Configuration)
}

type DisableMirroring struct{}

func (o DisableMirroring) Apply(dockerConfig *configuration.Configuration, extraConfig *registryconfig.Configuration) {
	extraConfig.Pullthrough.Mirror = false
}

type EnableMetrics struct {
	Secret string
}

func (o EnableMetrics) Apply(dockerConfig *configuration.Configuration, extraConfig *registryconfig.Configuration) {
	extraConfig.Metrics.Enabled = true
	extraConfig.Metrics.Secret = o.Secret
}

func StartTestRegistry(t *testing.T, kubeConfigPath string, options ...RegistryOption) (net.Listener, CloseFunc) {
	localIPv4, err := DefaultLocalIP4()
	if err != nil {
		t.Fatalf("failed to detect an IPv4 address which would be reachable from containers: %v", err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", localIPv4, 0))
	if err != nil {
		t.Fatalf("failed to listen on a port: %v", err)
	}

	dockerConfig := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"inmemory": configuration.Parameters{},
			"delete": configuration.Parameters{
				"enabled": true,
			},
		},
		Auth: configuration.Auth{
			"openshift": configuration.Parameters{},
		},
		Middleware: map[string][]configuration.Middleware{
			"registry":   {{Name: "openshift"}},
			"repository": {{Name: "openshift"}},
			"storage":    {{Name: "openshift"}},
		},
	}
	dockerConfig.Log.Level = "debug"

	extraConfig := &registryconfig.Configuration{
		KubeConfig: kubeConfigPath,
		Server: &registryconfig.Server{
			Addr: ln.Addr().String(),
		},
		Pullthrough: &registryconfig.Pullthrough{
			Enabled: true,
			Mirror:  true,
		},
		Quota: &registryconfig.Quota{
			Enabled:  false,
			CacheTTL: 1 * time.Minute,
		},
		Cache: &registryconfig.Cache{
			BlobRepositoryTTL: 10 * time.Minute,
		},
		Compatibility: &registryconfig.Compatibility{
			AcceptSchema2: true,
		},
	}

	for _, opt := range options {
		opt.Apply(dockerConfig, extraConfig)
	}

	if err := registryconfig.InitExtraConfig(dockerConfig, extraConfig); err != nil {
		t.Fatalf("unable to init registry config: %v", err)
	}

	ctx := context.Background()
	ctx = testutil.WithTestLogger(ctx, t)
	srv, err := dockerregistry.NewServer(ctx, dockerConfig, extraConfig)
	if err != nil {
		t.Fatalf("failed to create a new server: %v", err)
	}

	closed := int32(0)
	go func() {
		err := srv.Serve(ln)
		if atomic.LoadInt32(&closed) == 0 {
			// We cannot call t.Fatal here, because it's a different goroutine.
			panic(fmt.Errorf("failed to serve the image registry: %v", err))
		}
	}()
	close := func() error {
		atomic.StoreInt32(&closed, 1)
		return ln.Close()
	}

	return ln, close
}

type Registry struct {
	t        *testing.T
	listener net.Listener
	closeFn  CloseFunc
}

func (r *Registry) Close() {
	if err := r.closeFn(); err != nil {
		r.t.Fatalf("failed to close the registry's listener: %v", err)
	}
}

func (r *Registry) BaseURL() string {
	return "http://" + r.listener.Addr().String()
}

func (r *Registry) Repository(namespace string, imagestream string, user *User) *Repository {
	creds := testutil.NewBasicCredentialStore(user.Name, user.Token)

	baseURL := r.BaseURL()
	repoName := fmt.Sprintf("%s/%s", namespace, imagestream)

	transport, err := testutil.NewTransport(baseURL, repoName, creds)
	if err != nil {
		r.t.Fatalf("failed to get transport for %s: %v", repoName, err)
	}

	repo, err := testutil.NewRepository(repoName, baseURL, transport)
	if err != nil {
		r.t.Fatalf("failed to get repository %s: %v", repoName, err)
	}

	return &Repository{
		Repository: repo,
		baseURL:    baseURL,
		repoName:   repoName,
		transport:  transport,
	}
}
