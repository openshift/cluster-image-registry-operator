package testframework

import (
	"errors"
	"net"
	"strconv"
)

// ErrNoDefaultIP is returned when no suitable non-loopback address can be found.
var ErrNoDefaultIP = errors.New("no suitable IP address")

// DefaultLocalIP4 returns an IPv4 address that this host can be reached
// on. Will return ErrNoDefaultIP if no suitable address can be found.
//
// github.com/openshift/origin/pkg/cmd/util.DefaultLocalIP4
func DefaultLocalIP4() (net.IP, error) {
	devices, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, dev := range devices {
		if (dev.Flags&net.FlagUp != 0) && (dev.Flags&net.FlagLoopback == 0) {
			addrs, err := dev.Addrs()
			if err != nil {
				continue
			}
			for i := range addrs {
				if ip, ok := addrs[i].(*net.IPNet); ok {
					if ip.IP.To4() != nil {
						return ip.IP, nil
					}
				}
			}
		}
	}
	return nil, ErrNoDefaultIP
}

// FindFreeLocalPort returns the number of an available port number on
// the loopback interface.  Useful for determining the port to launch
// a server on.  Error handling required - there is a non-zero chance
// that the returned port number will be bound by another process
// after this function returns.
//
// k8s.io/kubernetes/test/integration/framework.FindFreeLocalPort
func FindFreeLocalPort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer func() {
		// ignore error
		_ = l.Close()
	}()
	_, portStr, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, err
	}
	return port, nil
}
