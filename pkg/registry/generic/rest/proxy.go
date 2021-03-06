/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rest

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util/httpstream"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util/proxy"

	"github.com/GoogleCloudPlatform/kubernetes/third_party/golang/netutil"
	"github.com/golang/glog"
)

// UpgradeAwareProxyHandler is a handler for proxy requests that may require an upgrade
type UpgradeAwareProxyHandler struct {
	UpgradeRequired bool
	Location        *url.URL
	Transport       http.RoundTripper
	FlushInterval   time.Duration
	err             error
}

const defaultFlushInterval = 200 * time.Millisecond

// NewUpgradeAwareProxyHandler creates a new proxy handler with a default flush interval
func NewUpgradeAwareProxyHandler(location *url.URL, transport http.RoundTripper, upgradeRequired bool) *UpgradeAwareProxyHandler {
	return &UpgradeAwareProxyHandler{
		Location:        location,
		Transport:       transport,
		UpgradeRequired: upgradeRequired,
		FlushInterval:   defaultFlushInterval,
	}
}

// RequestError returns an error that occurred while handling request
func (h *UpgradeAwareProxyHandler) RequestError() error {
	return h.err
}

// ServeHTTP handles the proxy request
func (h *UpgradeAwareProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.err = nil
	if len(h.Location.Scheme) == 0 {
		h.Location.Scheme = "http"
	}
	if h.tryUpgrade(w, req) {
		return
	}
	if h.UpgradeRequired {
		h.err = errors.NewBadRequest("Upgrade request required")
		return
	}

	if h.Transport == nil {
		h.Transport = h.defaultProxyTransport(req.URL)
	}

	loc := *h.Location
	loc.RawQuery = req.URL.RawQuery
	newReq, err := http.NewRequest(req.Method, loc.String(), req.Body)
	if err != nil {
		h.err = err
		return
	}
	newReq.Header = req.Header

	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: h.Location.Scheme, Host: h.Location.Host})
	proxy.Transport = h.Transport
	proxy.FlushInterval = h.FlushInterval
	proxy.ServeHTTP(w, newReq)
}

// tryUpgrade returns true if the request was handled.
func (h *UpgradeAwareProxyHandler) tryUpgrade(w http.ResponseWriter, req *http.Request) bool {
	if !httpstream.IsUpgradeRequest(req) {
		return false
	}

	backendConn, err := h.dialURL()
	if err != nil {
		h.err = err
		return true
	}
	defer backendConn.Close()

	requestHijackedConn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		h.err = err
		return true
	}
	defer requestHijackedConn.Close()

	newReq, err := http.NewRequest(req.Method, h.Location.String(), req.Body)
	if err != nil {
		h.err = err
		return true
	}
	newReq.Header = req.Header

	if err = newReq.Write(backendConn); err != nil {
		h.err = err
		return true
	}

	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
		_, err := io.Copy(backendConn, requestHijackedConn)
		if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			glog.Errorf("Error proxying data from client to backend: %v", err)
		}
		wg.Done()
	}()

	go func() {
		_, err := io.Copy(requestHijackedConn, backendConn)
		if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			glog.Errorf("Error proxying data from backend to client: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()
	return true
}

func (h *UpgradeAwareProxyHandler) dialURL() (net.Conn, error) {
	dialAddr := netutil.CanonicalAddr(h.Location)

	switch h.Location.Scheme {
	case "http":
		return net.Dial("tcp", dialAddr)
	case "https":
		// Get the tls config from the transport if we recognize it
		var tlsConfig *tls.Config
		if h.Transport != nil {
			httpTransport, ok := h.Transport.(*http.Transport)
			if ok {
				tlsConfig = httpTransport.TLSClientConfig
			}
		}

		// Dial
		tlsConn, err := tls.Dial("tcp", dialAddr, tlsConfig)
		if err != nil {
			return nil, err
		}

		// Verify
		host, _, _ := net.SplitHostPort(dialAddr)
		if err := tlsConn.VerifyHostname(host); err != nil {
			tlsConn.Close()
			return nil, err
		}

		return tlsConn, nil
	default:
		return nil, fmt.Errorf("Unknown scheme: %s", h.Location.Scheme)
	}
}

func (h *UpgradeAwareProxyHandler) defaultProxyTransport(url *url.URL) http.RoundTripper {
	scheme := url.Scheme
	host := url.Host
	pathPrepend := strings.TrimRight(url.Path, h.Location.Path)
	return &proxy.Transport{
		Scheme:      scheme,
		Host:        host,
		PathPrepend: pathPrepend,
	}
}
