package mihomo

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestGetJSONUsesUnixSocketAndGET(t *testing.T) {
	socketPath := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/proxies/test-group" {
			t.Fatalf("path = %q, want /proxies/test-group", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test-group","type":"Selector","all":["node-a","node-b"],"now":"node-a"}`))
	}))

	client := NewClient(socketPath)
	var group ProxyGroupInfo
	if err := client.GetJSON(context.Background(), "/proxies/test-group", &group); err != nil {
		t.Fatalf("GetJSON returned error: %v", err)
	}

	if group.Name != "test-group" {
		t.Fatalf("group name = %q, want test-group", group.Name)
	}
	if got := len(group.All); got != 2 {
		t.Fatalf("len(group.All) = %d, want 2", got)
	}
}

func TestGetJSONMissingSocketReturnsError(t *testing.T) {
	client := NewClient(filepath.Join(t.TempDir(), "missing.sock"))
	var out map[string]any

	err := client.GetJSON(context.Background(), "/proxies/test-group", &out)
	if err == nil {
		t.Fatal("GetJSON returned nil error, want missing socket error")
	}
}

func TestNewTCPClientGetJSONUsesTCPBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/proxies/test-group" {
			t.Fatalf("path = %q, want /proxies/test-group", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test-group","type":"Selector","all":["node-a"],"now":"node-a"}`))
	}))
	t.Cleanup(server.Close)

	client := NewTCPClient(server.URL)
	var group ProxyGroupInfo
	if err := client.GetJSON(context.Background(), "proxies/test-group", &group); err != nil {
		t.Fatalf("GetJSON returned error: %v", err)
	}
	if group.Name != "test-group" {
		t.Fatalf("group.Name = %q, want test-group", group.Name)
	}
}

func TestProxyGroupsListsGroupEntriesSortedByName(t *testing.T) {
	socketPath := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/proxies" {
			t.Fatalf("path = %q, want /proxies", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"proxies":{"node-a":{"name":"node-a","type":"Shadowsocks"},"group-b":{"name":"group-b","type":"Selector","all":["node-a"]},"group-a":{"type":"URLTest","all":["node-b","node-c"]}}}`))
	}))

	client := NewClient(socketPath)
	groups, err := client.ProxyGroups(context.Background())
	if err != nil {
		t.Fatalf("ProxyGroups returned error: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if groups[0].Name != "group-a" || groups[0].Type != "URLTest" || len(groups[0].All) != 2 {
		t.Fatalf("groups[0] = %#v", groups[0])
	}
	if groups[1].Name != "group-b" || groups[1].Type != "Selector" || len(groups[1].All) != 1 {
		t.Fatalf("groups[1] = %#v", groups[1])
	}
}

func TestSelectNodePutsNamePayloadToTCPController(t *testing.T) {
	requests := make(chan struct {
		method string
		path   string
		body   string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		requests <- struct {
			method string
			path   string
			body   string
		}{r.Method, r.URL.EscapedPath(), string(body)}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	client := NewTCPClient(server.URL + "/")
	if err := client.SelectNode(context.Background(), "Auto Select", "node/a b"); err != nil {
		t.Fatalf("SelectNode returned error: %v", err)
	}

	got := <-requests
	if got.method != http.MethodPut {
		t.Fatalf("method = %s, want PUT", got.method)
	}
	if got.path != "/proxies/Auto%20Select" {
		t.Fatalf("path = %q, want /proxies/Auto%%20Select", got.path)
	}
	if got.body != `{"name":"node/a b"}`+"\n" {
		t.Fatalf("body = %q, want name JSON payload", got.body)
	}
}

func TestDelayEscapesNodePathAndQuery(t *testing.T) {
	requests := make(chan *url.URL, 1)
	socketPath := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		requests <- r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delay":123}`))
	}))

	client := NewClient(socketPath)
	result, err := client.Delay(context.Background(), "node/a b?c", "https://example.com/health?x=1&y=two", 2500)
	if err != nil {
		t.Fatalf("Delay returned error: %v", err)
	}
	if result.Delay != 123 {
		t.Fatalf("delay = %d, want 123", result.Delay)
	}

	got := <-requests
	if got.EscapedPath() != "/proxies/node%2Fa%20b%3Fc/delay" {
		t.Fatalf("escaped path = %q, want /proxies/node%%2Fa%%20b%%3Fc/delay", got.EscapedPath())
	}
	if got.Query().Get("url") != "https://example.com/health?x=1&y=two" {
		t.Fatalf("url query = %q", got.Query().Get("url"))
	}
	if got.Query().Get("timeout") != "2500" {
		t.Fatalf("timeout query = %q, want 2500", got.Query().Get("timeout"))
	}
}

func startUnixHTTPServer(t *testing.T, handler http.Handler) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "browsebox-mihomo-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "m.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("server.Serve returned error: %v", err)
		}
	}()

	t.Cleanup(func() {
		_ = server.Close()
		<-done
		_ = os.Remove(socketPath)
	})

	return socketPath
}
