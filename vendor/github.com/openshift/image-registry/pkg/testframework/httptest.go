package testframework

import (
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
)

type HTTPServer struct {
	Listener net.Listener
	URL      *url.URL

	t      *testing.T
	closed int32
}

func NewHTTPServer(t *testing.T, handler http.Handler) *HTTPServer {
	localIPv4, err := DefaultLocalIP4()
	if err != nil {
		t.Fatal(err)
	}

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}

	_, portStr, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	addr := net.JoinHostPort(localIPv4.String(), portStr)

	hs := &HTTPServer{
		Listener: l,
		URL: &url.URL{
			Scheme: "http",
			Host:   addr,
		},
		t: t,
	}

	go func() {
		err := http.Serve(l, handler)
		if atomic.LoadInt32(&hs.closed) == 0 {
			t.Errorf("failed to serve: %v", err)
		}
	}()

	return hs
}

func (hs *HTTPServer) Close() {
	atomic.StoreInt32(&hs.closed, 1)
	if err := hs.Listener.Close(); err != nil {
		hs.t.Errorf("failed to close listener: %v", err)
	}
}
