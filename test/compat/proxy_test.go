//go:build compat

package compat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// request is one call the operator made to an application. Before holds the
// resource as the application served it just ahead of a PUT.
type request struct {
	Method string
	Path   string
	Body   string
	Before string
}

func (r request) String() string {
	if r.Before != "" {
		if changes := jsonChanges(r.Before, r.Body); len(changes) > 0 {
			return fmt.Sprintf("%s %s changes:\n      %s", r.Method, r.Path, strings.Join(changes, "\n      "))
		}
	}
	if r.Body == "" {
		return r.Method + " " + r.Path
	}
	body := r.Body
	if len(body) > 1500 {
		body = body[:1500] + "…"
	}
	return fmt.Sprintf("%s %s %s", r.Method, r.Path, body)
}

// jsonChanges lists the paths where after differs from before, so a failed
// idempotency check names the setting the application stores differently.
func jsonChanges(before, after string) []string {
	var b, a any
	if json.Unmarshal([]byte(before), &b) != nil || json.Unmarshal([]byte(after), &a) != nil {
		return nil
	}
	var out []string
	var walk func(path string, b, a any)
	walk = func(path string, b, a any) {
		switch av := a.(type) {
		case map[string]any:
			bv, ok := b.(map[string]any)
			if !ok {
				break
			}
			keys := make([]string, 0, len(av))
			for k := range av {
				keys = append(keys, k)
			}
			for k := range bv {
				if _, ok := av[k]; !ok {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(path+"."+k, bv[k], av[k])
			}
			return
		case []any:
			bv, ok := b.([]any)
			if !ok || len(bv) != len(av) {
				out = append(out, fmt.Sprintf("%s: %d items -> %d items", strings.TrimPrefix(path, "."), len(bv), len(av)))
				return
			}
			for i := range av {
				walk(fmt.Sprintf("%s[%d]", path, i), bv[i], av[i])
			}
			return
		}
		if !reflect.DeepEqual(a, b) {
			bj, _ := json.Marshal(b)
			aj, _ := json.Marshal(a)
			out = append(out, fmt.Sprintf("%s: %s -> %s",
				strings.TrimPrefix(path, "."), truncate(string(bj)), truncate(string(aj))))
		}
	}
	walk("", b, a)
	return out
}

func truncate(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// isRead reports whether a request leaves application state unchanged. Most
// APIs read with GET; apps that read or authenticate with POST supply their own.
type isRead func(request) bool

func getOnly(r request) bool { return r.Method == http.MethodGet || r.Method == http.MethodHead }

// recordingProxy sits between the operator and one application so the suite can
// assert what the operator sent, independent of which metrics an integration emits.
type recordingProxy struct {
	mu       sync.Mutex
	requests []request
	server   *httptest.Server
}

func newRecordingProxy(t *testing.T, target string) *recordingProxy {
	t.Helper()
	upstream, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingProxy{}
	forward := httputil.NewSingleHostReverseProxy(upstream)
	p.server = serveOnHostIP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := request{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)}
		if r.Method == http.MethodPut {
			rec.Before = fetchBefore(target, r)
		}
		p.mu.Lock()
		p.requests = append(p.requests, rec)
		p.mu.Unlock()
		forward.ServeHTTP(w, r)
	}))
	return p
}

// fetchBefore reads the resource a PUT is about to replace, with the operator's
// own credentials, so a failure can show what changed.
func fetchBefore(target string, put *http.Request) string {
	req, err := http.NewRequestWithContext(put.Context(), http.MethodGet, target+put.URL.RequestURI(), nil)
	if err != nil {
		return ""
	}
	req.Header = put.Header.Clone()
	req.Header.Del("Content-Length")
	req.Header.Del("Content-Type")
	// An explicit Accept-Encoding stops the transport from decompressing.
	req.Header.Del("Accept-Encoding")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func (p *recordingProxy) url() string { return p.server.URL }

func (p *recordingProxy) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = nil
}

// writes returns every recorded request the read predicate does not accept.
func (p *recordingProxy) writes(read isRead) []request {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []request
	for _, r := range p.requests {
		if !read(r) {
			out = append(out, r)
		}
	}
	return out
}

// serveOnHostIP listens on the host's outbound address: the operator's HTTP
// client refuses loopback, and containers reach the host on this address too.
func serveOnHostIP(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(hostIP, "0"))
	if err != nil {
		t.Fatalf("listening on %s: %v", hostIP, err)
	}
	srv := httptest.NewUnstartedServer(handler)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}
