package testframework

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

func WaitTCP(addr string) error {
	var lastErr error
	err := wait.Poll(500*time.Millisecond, wait.ForeverTestTimeout, func() (done bool, err error) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			lastErr = err
			return false, nil
		}
		_ = conn.Close()
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("wait for %s: %v", addr, lastErr)
	}
	return nil
}

func WaitHTTP(rt http.RoundTripper, url string) error {
	var lastErr error
	httpClient := &http.Client{
		Transport: rt,
	}
	err := wait.Poll(500*time.Millisecond, time.Second*120, func() (done bool, err error) {
		resp, err := httpClient.Get(url)
		if err != nil {
			lastErr = err
			return false, nil
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s", resp.Status)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("wait for %s: %v", url, lastErr)
	}
	return nil
}
