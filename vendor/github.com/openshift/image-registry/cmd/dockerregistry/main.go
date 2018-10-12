package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apiserver/pkg/util/logs"

	"github.com/openshift/library-go/pkg/serviceability"

	"github.com/openshift/image-registry/pkg/cmd/dockerregistry"
	"github.com/openshift/image-registry/pkg/version"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()
	defer serviceability.BehaviorOnPanic(os.Getenv("OPENSHIFT_ON_PANIC"), version.Get())()
	defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()
	startProfiler()

	rand.Seed(time.Now().UTC().UnixNano())
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()

	// TODO convert to flags instead of a config file?
	configurationPath := ""
	if flag.NArg() > 0 {
		configurationPath = flag.Arg(0)
	}
	if configurationPath == "" {
		configurationPath = os.Getenv("REGISTRY_CONFIGURATION_PATH")
	}

	if configurationPath == "" {
		fmt.Println("configuration path unspecified")
		os.Exit(1)
	}
	// Prevent a warning about unrecognized environment variable
	if err := os.Unsetenv("REGISTRY_CONFIGURATION_PATH"); err != nil {
		log.Fatalf("Unable to unset REGISTRY_CONFIGURATION_PATH: %v", err)
	}

	configFile, err := os.Open(configurationPath)
	if err != nil {
		log.Fatalf("Unable to open configuration file: %s", err)
	}

	dockerregistry.Execute(configFile)
}

func env(key string, defaultValue string) string {
	val := os.Getenv(key)
	if len(val) == 0 {
		return defaultValue
	}
	return val
}

func startProfiler() {
	if env("OPENSHIFT_PROFILE", "") == "web" {
		go func() {
			runtime.SetBlockProfileRate(1)
			profilePort := env("OPENSHIFT_PROFILE_PORT", "6060")
			profileHost := env("OPENSHIFT_PROFILE_HOST", "127.0.0.1")
			log.Infof(fmt.Sprintf("Starting profiling endpoint at http://%s:%s/debug/pprof/", profileHost, profilePort))
			log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%s", profileHost, profilePort), nil))
		}()
	}
}
