package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/docker/distribution"
	dockercfg "github.com/docker/distribution/configuration"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/auth"

	authorizationapi "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	restclient "k8s.io/client-go/rest"

	userapi "github.com/openshift/api/user/v1"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	"github.com/openshift/image-registry/pkg/origin-common/clientcmd"
	"github.com/openshift/image-registry/pkg/testutil"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

func init() {
	authorizationapi.AddToScheme(scheme)
	userapi.AddToScheme(scheme)

}

func sarResponse(ns string, allowed bool, reason string) *authorizationapi.SelfSubjectAccessReview {
	resp := &authorizationapi.SelfSubjectAccessReview{}
	resp.Namespace = ns
	resp.Status = authorizationapi.SubjectAccessReviewStatus{Allowed: allowed, Reason: reason}
	return resp
}

// TestVerifyImageStreamAccess mocks openshift http request/response and
// tests invalid/valid/scoped openshift tokens.
func TestVerifyImageStreamAccess(t *testing.T) {
	tests := []struct {
		openshiftResponse response
		expectedError     error
	}{
		{
			// Test invalid openshift bearer token
			openshiftResponse: response{401, "Unauthorized"},
			expectedError:     ErrOpenShiftAccessDenied,
		},
		{
			// Test valid openshift bearer token but token *not* scoped for create operation
			openshiftResponse: response{
				200,
				runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("foo", false, "not authorized!")),
			},
			expectedError: ErrOpenShiftAccessDenied,
		},
		{
			// Test valid openshift bearer token and token scoped for create operation
			openshiftResponse: response{
				200,
				runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("foo", true, "authorized!")),
			},
			expectedError: nil,
		},
	}
	for _, test := range tests {
		ctx := context.Background()
		ctx = testutil.WithTestLogger(ctx, t)
		server, _ := simulateOpenShiftMaster([]response{test.openshiftResponse})

		cfg := clientcmd.NewConfig()
		cfg.SkipEnv = true
		cfg.KubernetesAddr.Set(server.URL)
		cfg.CommonConfig = restclient.Config{
			BearerToken: "magic bearer token",
			Host:        server.URL,
		}
		osclient, err := client.NewRegistryClient(cfg).Client()
		if err != nil {
			t.Fatal(err)
		}
		err = verifyImageStreamAccess(ctx, "foo", "bar", "create", osclient)
		if err == nil || test.expectedError == nil {
			if err != test.expectedError {
				t.Fatalf("verifyImageStreamAccess did not get expected error - got %s - expected %s", err, test.expectedError)
			}
		} else if err.Error() != test.expectedError.Error() {
			t.Fatalf("verifyImageStreamAccess did not get expected error - got %s - expected %s", err, test.expectedError)
		}
		server.Close()
	}
}

// TestAccessController tests complete integration of the v2 registry auth package.
func TestAccessController(t *testing.T) {
	const addr = "https://openshift-example.com/osapi"

	authConfig := &configuration.Auth{
		Realm:      "myrealm",
		TokenRealm: "http://tokenrealm.com",
	}

	tests := map[string]struct {
		authConfig         *configuration.Auth
		access             []auth.Access
		basicToken         string
		bearerToken        string
		openshiftResponses []response
		expectedError      error
		expectedChallenge  bool
		expectedHeaders    http.Header
		expectedRepoErr    string
		expectedActions    []string
	}{
		"no token": {
			access:            []auth.Access{},
			basicToken:        "",
			expectedError:     ErrTokenRequired,
			expectedChallenge: true,
			expectedHeaders:   http.Header{"Www-Authenticate": []string{`Bearer realm="http://tokenrealm.com/openshift/token"`}},
		},
		"no token, autodetected tokenrealm": {
			authConfig: &configuration.Auth{
				Realm:      "myrealm",
				TokenRealm: "",
			},
			access:            []auth.Access{},
			basicToken:        "",
			expectedError:     ErrTokenRequired,
			expectedChallenge: true,
			expectedHeaders:   http.Header{"Www-Authenticate": []string{`Bearer realm="https://openshift-example.com/openshift/token"`}},
		},
		"invalid registry token": {
			access: []auth.Access{{
				Resource: auth.Resource{Type: "repository"},
			}},
			basicToken:        "ab-cd-ef-gh",
			expectedError:     ErrTokenInvalid,
			expectedChallenge: true,
			expectedHeaders:   http.Header{"Www-Authenticate": []string{`Basic realm=myrealm,error="failed to decode credentials"`}},
		},
		"invalid openshift basic password": {
			access: []auth.Access{{
				Resource: auth.Resource{Type: "repository"},
			}},
			basicToken:        "abcdefgh",
			expectedError:     ErrTokenInvalid,
			expectedChallenge: true,
			expectedHeaders:   http.Header{"Www-Authenticate": []string{`Basic realm=myrealm,error="failed to decode credentials"`}},
		},
		"valid openshift token but invalid namespace": {
			access: []auth.Access{{
				Resource: auth.Resource{
					Type: "repository",
					Name: "bar",
				},
				Action: "pull",
			}},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
			},
			expectedError: distribution.ErrRepositoryNameInvalid{
				Name:   "bar",
				Reason: fmt.Errorf("it must be of the format <project>/<name>"),
			},
			expectedChallenge: false,
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
			},
		},
		"registry token but does not involve any repository operation": {
			access:     []auth.Access{{}},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
			},
			expectedError:     ErrUnsupportedResource,
			expectedChallenge: false,
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
			},
		},
		"registry token but does not involve any known action": {
			access: []auth.Access{{
				Resource: auth.Resource{
					Type: "repository",
					Name: "foo/bar",
				},
				Action: "blah",
			}},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
			},
			expectedError:     ErrUnsupportedAction,
			expectedChallenge: false,
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
			},
		},
		"docker login with invalid openshift creds": {
			basicToken:         "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{{403, ""}},
			expectedError:      ErrOpenShiftAccessDenied,
			expectedChallenge:  true,
			expectedHeaders:    http.Header{"Www-Authenticate": []string{`Basic realm=myrealm,error="access denied"`}},
			expectedActions:    []string{"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)"},
		},
		"docker login with valid openshift creds": {
			basicToken: "dXNyMTphd2Vzb21l",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
			},
			expectedError:     nil,
			expectedChallenge: false,
			expectedActions:   []string{"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)"},
		},
		"error running subject access review": {
			access: []auth.Access{{
				Resource: auth.Resource{
					Type: "repository",
					Name: "foo/bar",
				},
				Action: "pull",
			}},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
				{500, "Uh oh"},
			},
			expectedError:     errors.New("an error on the server (\"unknown\") has prevented the request from succeeding (post selfsubjectaccessreviews.authorization.k8s.io)"),
			expectedChallenge: false,
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
			},
		},
		"valid openshift token but token not scoped for the given repo operation": {
			access: []auth.Access{{
				Resource: auth.Resource{
					Type: "repository",
					Name: "foo/bar",
				},
				Action: "pull",
			}},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("foo", false, "not"))},
			},
			expectedError:     ErrOpenShiftAccessDenied,
			expectedChallenge: true,
			expectedHeaders:   http.Header{"Www-Authenticate": []string{`Basic realm=myrealm,error="access denied"`}},
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
			},
		},
		"partially valid openshift token": {
			// Check all the different resource-type/verb combinations we allow to make sure they validate and continue to validate remaining Resource requests
			access: []auth.Access{
				{Resource: auth.Resource{Type: "repository", Name: "foo/aaa"}, Action: "pull"},
				{Resource: auth.Resource{Type: "repository", Name: "bar/bbb"}, Action: "push"},
				{Resource: auth.Resource{Type: "admin"}, Action: "prune"},
				{Resource: auth.Resource{Type: "repository", Name: "baz/ccc"}, Action: "push"},
			},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("foo", true, "authorized!"))},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("bar", true, "authorized!"))},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("", true, "authorized!"))},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("baz", false, "no!"))},
			},
			expectedError:     ErrOpenShiftAccessDenied,
			expectedChallenge: true,
			expectedHeaders:   http.Header{"Www-Authenticate": []string{`Basic realm=myrealm,error="access denied"`}},
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
			},
		},
		"deferred cross-mount error": {
			// cross-mount push requests check pull/push access on the target repo and pull access on the source repo.
			// we expect the access check failure for fromrepo/bbb to be added to the context as a deferred error,
			// which our blobstore will look for and prevent a cross mount from.
			access: []auth.Access{
				{Resource: auth.Resource{Type: "repository", Name: "pushrepo/aaa"}, Action: "pull"},
				{Resource: auth.Resource{Type: "repository", Name: "pushrepo/aaa"}, Action: "push"},
				{Resource: auth.Resource{Type: "repository", Name: "fromrepo/bbb"}, Action: "pull"},
			},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("pushrepo", true, "authorized!"))},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("pushrepo", true, "authorized!"))},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("fromrepo", false, "no!"))},
			},
			expectedError:     nil,
			expectedChallenge: false,
			expectedRepoErr:   "fromrepo/bbb",
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
			},
		},
		"valid openshift token": {
			access: []auth.Access{{
				Resource: auth.Resource{
					Type: "repository",
					Name: "foo/bar",
				},
				Action: "pull",
			}},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("", true, "authorized!"))},
			},
			expectedError:     nil,
			expectedChallenge: false,
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
			},
		},
		"valid anonymous token": {
			access: []auth.Access{{
				Resource: auth.Resource{
					Type: "repository",
					Name: "foo/bar",
				},
				Action: "pull",
			}},
			bearerToken: "anonymous",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("foo", true, "authorized!"))},
			},
			expectedError:     nil,
			expectedChallenge: false,
			expectedActions: []string{
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=)",
			},
		},
		"pruning": {
			access: []auth.Access{
				{
					Resource: auth.Resource{
						Type: "admin",
					},
					Action: "prune",
				},
				{
					Resource: auth.Resource{
						Type: "repository",
						Name: "foo/bar",
					},
					Action: "delete",
				},
			},
			basicToken: "b3BlbnNoaWZ0OmF3ZXNvbWU=",
			openshiftResponses: []response{
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(userapi.SchemeGroupVersion), &userapi.User{ObjectMeta: metav1.ObjectMeta{Name: "usr1"}})},
				{200, runtime.EncodeOrDie(codecs.LegacyCodec(authorizationapi.SchemeGroupVersion), sarResponse("", true, "authorized!"))},
			},
			expectedError:     nil,
			expectedChallenge: false,
			expectedActions: []string{
				"GET /apis/user.openshift.io/v1/users/~ (Authorization=Bearer awesome)",
				"POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews (Authorization=Bearer awesome)",
			},
		},
	}

	for k, test := range tests {
		t.Run(k, func(t *testing.T) {
			reqURL, err := url.Parse(addr)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest("GET", addr, nil)
			if err != nil {
				t.Fatalf("%s: %v", k, err)
			}
			// Simulate a secure request to the specified server
			req.Host = reqURL.Host
			req.TLS = &tls.ConnectionState{ServerName: reqURL.Host}
			if len(test.basicToken) > 0 {
				req.Header.Set("Authorization", fmt.Sprintf("Basic %s", test.basicToken))
			}
			if len(test.bearerToken) > 0 {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", test.bearerToken))
			}

			ctx := context.Background()
			ctx = testutil.WithTestLogger(ctx, t)

			server, actions := simulateOpenShiftMaster(test.openshiftResponses)
			cfg := clientcmd.NewConfig()
			cfg.SkipEnv = true
			cfg.KubernetesAddr.Set(server.URL)
			cfg.CommonConfig = restclient.Config{
				Host:            server.URL,
				TLSClientConfig: restclient.TLSClientConfig{Insecure: true},
			}
			config := &dockercfg.Configuration{}
			app := &App{
				ctx:            ctx,
				registryClient: client.NewRegistryClient(cfg),
				config: &configuration.Configuration{
					Server: &configuration.Server{
						Addr: "localhost:5000",
					},
					Auth: test.authConfig,
				},
			}
			if app.config.Auth == nil {
				app.config.Auth = authConfig
			}
			if err := configuration.InitExtraConfig(config, app.config); err != nil {
				t.Fatal(err)
			}
			accessController, err := app.Auth(nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx = dcontext.WithRequest(ctx, req)
			authCtx, err := accessController.Authorized(ctx, test.access...)
			server.Close()

			expectedActions := test.expectedActions
			if expectedActions == nil {
				expectedActions = []string{}
			}
			if !reflect.DeepEqual(actions, &expectedActions) {
				t.Fatalf("expected: %#v, got: %#v", &expectedActions, actions)
			}

			if err == nil || test.expectedError == nil {
				if err != test.expectedError {
					t.Fatalf("accessController did not get expected error - got %#+v - expected %v", err, test.expectedError)
				}
				if authCtx == nil {
					t.Fatalf("expected auth context but got nil")
				}
				if !authPerformed(authCtx) {
					t.Fatalf("expected AuthPerformed to be true")
				}
				deferredErrors, hasDeferred := deferredErrorsFrom(authCtx)
				if len(test.expectedRepoErr) > 0 {
					if !hasDeferred || deferredErrors[test.expectedRepoErr] == nil {
						t.Fatalf("expected deferred error for repo %s, got none", test.expectedRepoErr)
					}
				} else {
					if hasDeferred && len(deferredErrors) > 0 {
						t.Fatalf("didn't expect deferred errors, got %#v", deferredErrors)
					}
				}
			} else {
				challengeErr, isChallenge := err.(auth.Challenge)
				if test.expectedChallenge != isChallenge {
					t.Fatalf("expected challenge=%v, accessController returned challenge=%v", test.expectedChallenge, isChallenge)
				}
				if isChallenge {
					recorder := httptest.NewRecorder()
					challengeErr.SetHeaders(recorder)
					if !reflect.DeepEqual(recorder.HeaderMap, test.expectedHeaders) {
						t.Fatalf("expected headers %#v, got %#v", test.expectedHeaders, recorder.HeaderMap)
					}
				}

				if err.Error() != test.expectedError.Error() {
					t.Fatalf("accessController did not get expected error - got %+v - expected %s", err, test.expectedError)
				}
				if authCtx != nil {
					t.Fatalf("expected nil auth context but got %s", authCtx)
				}
			}
		})
	}
}

type response struct {
	code int
	body string
}

func simulateOpenShiftMaster(responses []response) (*httptest.Server, *[]string) {
	i := 0
	actions := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := response{500, "No response registered"}
		if i < len(responses) {
			response = responses[i]
		}
		i++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.code)
		fmt.Fprintln(w, response.body)
		actions = append(actions, fmt.Sprintf(`%s %s (Authorization=%s)`, r.Method, r.URL.Path, r.Header.Get("Authorization")))
	}))
	return server, &actions
}
