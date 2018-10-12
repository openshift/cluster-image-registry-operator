package dockerregistry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	logrus_logstash "github.com/bshuster-repo/logrus-logstash-hook"
	log "github.com/sirupsen/logrus"

	"github.com/docker/distribution/configuration"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/health"
	"github.com/docker/distribution/uuid"
	distversion "github.com/docker/distribution/version"

	_ "github.com/docker/distribution/registry/auth/htpasswd"
	_ "github.com/docker/distribution/registry/auth/token"

	_ "github.com/docker/distribution/registry/proxy"
	_ "github.com/docker/distribution/registry/storage/driver/azure"
	_ "github.com/docker/distribution/registry/storage/driver/filesystem"
	_ "github.com/docker/distribution/registry/storage/driver/gcs"
	_ "github.com/docker/distribution/registry/storage/driver/inmemory"
	_ "github.com/docker/distribution/registry/storage/driver/middleware/cloudfront"
	_ "github.com/docker/distribution/registry/storage/driver/oss"
	_ "github.com/docker/distribution/registry/storage/driver/s3-aws"
	_ "github.com/docker/distribution/registry/storage/driver/swift"

	"github.com/openshift/library-go/pkg/crypto"

	"github.com/openshift/image-registry/pkg/dockerregistry/server"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/audit"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	registryconfig "github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/maxconnections"
	"github.com/openshift/image-registry/pkg/origin-common/clientcmd"
	"github.com/openshift/image-registry/pkg/version"
)

var experimental = flag.Bool("experimental", false, "enable experimental features")
var pruneMode = flag.String("prune", "", "prune blobs from the storage and exit (check, delete)")
var restoreMode = flag.String("restore-mode", "", "check data corruption or recover storage data if possible (valid values: check, check-database, check-storage, recover)")
var restoreNamespace = flag.String("restore-namespace", "", "check and recover only specified namespace")
var listRepositories = flag.Bool("list-repositories", false, "shows list of repositories")
var listBlobs = flag.Bool("list-blobs", false, "shows list of blob digests stored in the storage")
var listManifests = flag.Bool("list-manifests", false, "shows list of manifest digests stored in the storage")
var listRepositoryManifests = flag.String("list-manifests-from", "", "shows the manifest digests in the specified repository")

func versionFields() map[interface{}]interface{} {
	return map[interface{}]interface{}{
		"distribution_version": distversion.Version,
		"openshift_version":    version.Get(),
	}
}

func getListOptions() *ListOptions {
	opts := &ListOptions{
		Repositories: *listRepositories,
		Blobs:        *listBlobs,
		Manifests:    *listManifests,
	}

	if len(*listRepositoryManifests) > 0 {
		opts.Manifests = true
		opts.ManifestsFromRepo = *listRepositoryManifests
	}
	return opts
}

func optionsConflict() error {
	listOpts := getListOptions()

	if listOpts.Repositories || listOpts.Blobs || listOpts.Manifests {
		if !*experimental {
			return fmt.Errorf("options -list-repositories, -list-blobs, -list-manifests and -list-manifests-froms are experimental. Please specify the -experimental to use them.")
		}
		if len(*pruneMode) > 0 {
			return fmt.Errorf("options -list-repositories, -list-blobs, -list-manifests, -list-manifests-froms and -prune are mutually exclusive")
		}
		if len(*restoreMode) > 0 {
			return fmt.Errorf("options -list-repositories, -list-blobs, -list-manifests, -list-manifests-froms and -restore-mode are mutually exclusive")
		}
	}

	if len(*pruneMode) > 0 && len(*restoreMode) > 0 {
		return fmt.Errorf("options -prune and -restore-mode are mutually exclusive")
	}

	if len(*restoreMode) > 0 && !*experimental {
		return fmt.Errorf("option -restore-mode is experimental. Please specify the -experimental to use it.")
	}

	return nil
}

// Execute runs the Docker registry.
func Execute(configFile io.Reader) {
	if err := optionsConflict(); err != nil {
		log.Error(err)
		os.Exit(2)
	}

	listOpts := getListOptions()

	if listOpts.Repositories || listOpts.Blobs || listOpts.Manifests {
		ExecuteListFS(configFile, listOpts)
		return
	}

	if len(*restoreMode) != 0 {
		switch *restoreMode {
		case "check", "check-database", "check-storage", "recover":
			ExecuteRestore(configFile, *restoreMode, *restoreNamespace)
		default:
			log.Error("invalid value for the -restore-mode option")
			os.Exit(2)
		}
		return
	}

	if len(*pruneMode) != 0 {
		var dryRun bool
		switch *pruneMode {
		case "delete":
			dryRun = false
		case "check":
			dryRun = true
		default:
			log.Error("invalid value for the -prune option")
			os.Exit(2)
		}
		ExecutePruner(configFile, dryRun)
		return
	}

	dockerConfig, extraConfig, err := registryconfig.Parse(configFile)
	if err != nil {
		log.Fatalf("error parsing configuration file: %s", err)
	}

	ctx := context.Background()
	ctx, err = configureLogging(ctx, dockerConfig)
	if err != nil {
		log.Fatalf("error configuring logger: %v", err)
	}

	// inject a logger into the uuid library. warns us if there is a problem
	// with uuid generation under low entropy.
	uuid.Loggerf = dcontext.GetLogger(ctx).Warnf

	dcontext.GetLoggerWithFields(ctx, versionFields()).Info("start registry")

	srv, err := NewServer(ctx, dockerConfig, extraConfig)
	if err != nil {
		log.Fatal(err)
	}

	if dockerConfig.HTTP.TLS.Certificate == "" {
		dcontext.GetLogger(ctx).Infof("listening on %s", srv.Addr)
		err = srv.ListenAndServe()
	} else {
		dcontext.GetLogger(ctx).Infof("listening on %s, tls", srv.Addr)
		err = srv.ListenAndServeTLS(dockerConfig.HTTP.TLS.Certificate, dockerConfig.HTTP.TLS.Key)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func NewServer(ctx context.Context, dockerConfig *configuration.Configuration, extraConfig *registryconfig.Configuration) (*http.Server, error) {
	setDefaultLogParameters(dockerConfig)

	registryClient := client.NewRegistryClient(clientcmd.NewConfig().BindToFile(extraConfig.KubeConfig))

	readLimiter := newLimiter(extraConfig.Requests.Read)
	writeLimiter := newLimiter(extraConfig.Requests.Write)

	handler := server.NewApp(ctx, registryClient, dockerConfig, extraConfig, writeLimiter)
	handler = limit(readLimiter, writeLimiter, handler)
	handler = alive("/", handler)
	// TODO: temporarily keep for backwards compatibility; remove in the future
	handler = alive("/healthz", handler)
	handler = health.Handler(handler)
	handler = panicHandler(handler)
	if !dockerConfig.Log.AccessLog.Disabled {
		handler = logrusLoggingHandler(ctx, handler)
	}

	var tlsConf *tls.Config
	if dockerConfig.HTTP.TLS.Certificate != "" {
		var (
			minVersion   uint16
			cipherSuites []uint16
			err          error
		)
		if s := os.Getenv("REGISTRY_HTTP_TLS_MINVERSION"); len(s) > 0 {
			minVersion, err = crypto.TLSVersion(s)
			if err != nil {
				return nil, fmt.Errorf("invalid TLS version %q specified in REGISTRY_HTTP_TLS_MINVERSION: %v (valid values are %q)", s, err, crypto.ValidTLSVersions())
			}
		}
		if s := os.Getenv("REGISTRY_HTTP_TLS_CIPHERSUITES"); len(s) > 0 {
			for _, cipher := range strings.Split(s, ",") {
				cipherSuite, err := crypto.CipherSuite(cipher)
				if err != nil {
					return nil, fmt.Errorf("invalid cipher suite %q specified in REGISTRY_HTTP_TLS_CIPHERSUITES: %v (valid suites are %q)", s, err, crypto.ValidCipherSuites())
				}
				cipherSuites = append(cipherSuites, cipherSuite)
			}
		}
		tlsConf = crypto.SecureTLSConfig(&tls.Config{
			ClientAuth:   tls.NoClientCert,
			MinVersion:   minVersion,
			CipherSuites: cipherSuites,
		})

		if len(dockerConfig.HTTP.TLS.ClientCAs) != 0 {
			pool := x509.NewCertPool()

			for _, ca := range dockerConfig.HTTP.TLS.ClientCAs {
				caPem, err := ioutil.ReadFile(ca)
				if err != nil {
					return nil, err
				}

				if ok := pool.AppendCertsFromPEM(caPem); !ok {
					return nil, fmt.Errorf("could not add CA to pool")
				}
			}

			for _, subj := range pool.Subjects() {
				dcontext.GetLogger(ctx).Debugf("CA Subject: %s", string(subj))
			}

			tlsConf.ClientAuth = tls.RequireAndVerifyClientCert
			tlsConf.ClientCAs = pool
		}
	}

	return &http.Server{
		Addr:      dockerConfig.HTTP.Addr,
		Handler:   handler,
		TLSConfig: tlsConf,
	}, nil
}

// configureLogging prepares the context with a logger using the
// configuration.
func configureLogging(ctx context.Context, config *configuration.Configuration) (context.Context, error) {
	if config.Log.Level == "" && config.Log.Formatter == "" {
		// If no config for logging is set, fallback to deprecated "Loglevel".
		log.SetLevel(logLevel(config.Loglevel))
		ctx = dcontext.WithLogger(ctx, dcontext.GetLogger(ctx))
		return ctx, nil
	}

	log.SetLevel(logLevel(config.Log.Level))

	formatter := config.Log.Formatter
	if formatter == "" {
		formatter = "text" // default formatter
	}

	switch formatter {
	case "json":
		log.SetFormatter(&log.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
		})
	case "text":
		log.SetFormatter(&log.TextFormatter{
			TimestampFormat: time.RFC3339Nano,
		})
	case "logstash":
		log.SetFormatter(&logrus_logstash.LogstashFormatter{
			TimestampFormat: time.RFC3339Nano,
		})
	default:
		// just let the library use default on empty string.
		if config.Log.Formatter != "" {
			return ctx, fmt.Errorf("unsupported logging formatter: %q", config.Log.Formatter)
		}
	}

	if config.Log.Formatter != "" {
		log.Debugf("using %q logging formatter", config.Log.Formatter)
	}

	if len(config.Log.Fields) > 0 {
		// build up the static fields, if present.
		var fields []interface{}
		for k := range config.Log.Fields {
			fields = append(fields, k)
		}

		ctx = dcontext.WithValues(ctx, config.Log.Fields)
		ctx = dcontext.WithLogger(ctx, dcontext.GetLogger(ctx, fields...))
	}

	return ctx, nil
}

func logLevel(level configuration.Loglevel) log.Level {
	l, err := log.ParseLevel(string(level))
	if err != nil {
		l = log.InfoLevel
		log.Warnf("error parsing level %q: %v, using %q	", level, err, l)
	}

	return l
}

func newLimiter(c registryconfig.RequestsLimits) maxconnections.Limiter {
	if c.MaxRunning <= 0 {
		return nil
	}
	return maxconnections.NewLimiter(c.MaxRunning, c.MaxInQueue, c.MaxWaitInQueue)
}

func limit(readLimiter, writeLimiter maxconnections.Limiter, handler http.Handler) http.Handler {
	readHandler := handler
	if readLimiter != nil {
		readHandler = maxconnections.New(readLimiter, readHandler)
	}

	writeHandler := handler
	if writeLimiter != nil {
		writeHandler = maxconnections.New(writeLimiter, writeHandler)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.ToUpper(r.Method) {
		case "GET", "HEAD", "OPTIONS":
			readHandler.ServeHTTP(w, r)
		default:
			writeHandler.ServeHTTP(w, r)
		}
	})
}

// alive simply wraps the handler with a route that always returns an http 200
// response when the path is matched. If the path is not matched, the request
// is passed to the provided handler. There is no guarantee of anything but
// that the server is up. Wrap with other handlers (such as health.Handler)
// for greater affect.
func alive(path string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

// panicHandler add a HTTP handler to web app. The handler recover the happening
// panic. logrus.Panic transmits panic message to pre-config log hooks, which is
// defined in config.yml.
func panicHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Panic(fmt.Sprintf("%v", err))
			}
		}()
		handler.ServeHTTP(w, r)
	})
}

func setDefaultLogParameters(config *configuration.Configuration) {
	if len(config.Log.Fields) == 0 {
		config.Log.Fields = make(map[string]interface{})
	}
	config.Log.Fields[audit.LogEntryType] = audit.DefaultLoggerType
}

func logrusLoggingHandler(ctx context.Context, h http.Handler) http.Handler {
	return loggingHandler{
		ctx:     ctx,
		handler: h,
	}
}

type loggingHandler struct {
	ctx     context.Context
	handler http.Handler
}

func (h loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx

	ctx = dcontext.WithRequest(ctx, r)
	ctx, w = dcontext.WithResponseWriter(ctx, w)
	logger := dcontext.GetRequestLogger(ctx)
	ctx = dcontext.WithLogger(ctx, logger)

	h.handler.ServeHTTP(w, r)

	dcontext.GetResponseLogger(ctx).Infof("response")
}
