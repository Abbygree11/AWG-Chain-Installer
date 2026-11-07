package chain

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// packageRelay makes the installer's own internet connection available to
// apt/curl on the VPS, *only* through an authenticated SSH reverse forward.
// No port is published publicly. APT still verifies signed repository metadata.
type packageRelay struct {
	listener net.Listener
	server   *http.Server
}

var allowedPackageHosts = map[string]bool{
	"ppa.launchpadcontent.net": true,
	"ppa.launchpad.net":        true,
	"keyserver.ubuntu.com":     true,
}

func packageProxyDestination(raw string) (string, bool) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return "", false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if !allowedPackageHosts[host] || (port != "443" && port != "80") {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

func packageProxyHandler() http.Handler {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConnsPerHost:   4,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodConnect {
			connectPackageProxy(w, req)
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.Error(w, "method not permitted", http.StatusMethodNotAllowed)
			return
		}
		if req.URL == nil || !req.URL.IsAbs() || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			http.Error(w, "absolute URL required", http.StatusBadRequest)
			return
		}
		host := strings.ToLower(strings.TrimSuffix(req.URL.Hostname(), "."))
		if !allowedPackageHosts[host] {
			http.Error(w, "repository hostname not permitted", http.StatusForbidden)
			return
		}
		if req.URL.User != nil || (req.URL.Port() != "" && req.URL.Port() != "80" && req.URL.Port() != "443") {
			http.Error(w, "invalid repository URL", http.StatusBadRequest)
			return
		}
		out := req.Clone(req.Context())
		out.RequestURI = ""
		out.Header.Del("Proxy-Authorization")
		out.Header.Del("Proxy-Connection")
		out.Header.Del("Connection")
		out.Header.Del("Keep-Alive")
		out.Header.Del("Transfer-Encoding")
		out.Header.Del("Upgrade")
		out.Header.Del("TE")
		out.Header.Del("Trailer")
		out.Host = out.URL.Host
		resp, err := transport.RoundTrip(out)
		if err != nil {
			http.Error(w, "cannot fetch signed repository from this computer", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, values := range resp.Header {
			switch strings.ToLower(k) {
			case "connection", "transfer-encoding", "proxy-authenticate", "proxy-authorization", "keep-alive", "upgrade":
				continue
			}
			for _, value := range values {
				w.Header().Add(k, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}

func connectPackageProxy(w http.ResponseWriter, req *http.Request) {
	dest, allowed := packageProxyDestination(req.Host)
	if !allowed {
		http.Error(w, "destination not permitted", http.StatusForbidden)
		return
	}
	// The dial happens on the desktop running the GUI, not on the VPS.
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(req.Context(), "tcp", dest)
	if err != nil {
		http.Error(w, "destination unreachable from local computer", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = conn.Close()
		http.Error(w, "CONNECT unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hj.Hijack()
	if err != nil {
		_ = conn.Close()
		return
	}
	_, err = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if err != nil {
		_ = client.Close()
		_ = conn.Close()
		return
	}
	var once sync.Once
	closeBoth := func() {once.Do(func() {_ = conn.Close();_ = client.Close()})}
	go func() {
		_, _ = io.Copy(conn, buffered)
		closeBoth()
	}()
	go func() {
		_, _ = io.Copy(client, conn)
		closeBoth()
	}()
}

func newPackageRelay(r *Remote) (*packageRelay, string, error) {
	// 127.0.0.1:0 asks sshd to choose an unused port on the VPS.
	// This is a remote socket bound only on loopback, not 0.0.0.0.
	listener, err := r.client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("SSH-сервер запретил обратное перенаправление портов: %w", err)
	}
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil || host != "127.0.0.1" {
		_ = listener.Close()
		return nil, "", errors.New("SSH-сервер не подтвердил безопасный адрес 127.0.0.1 для прокси")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		_ = listener.Close()
		return nil, "", errors.New("не удалось получить SSH-порт обратного прокси")
	}
	srv := &http.Server{
		Handler:           packageProxyHandler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       40 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	relay := &packageRelay{listener: listener, server: srv}
	go func() {
		_ = srv.Serve(listener)
	}()
	return relay, (&url.URL{Scheme:"http", Host:net.JoinHostPort("127.0.0.1", port)}).String(), nil
}

func (p *packageRelay) Close() error {
	_ = p.server.Close()
	return p.listener.Close()
}

