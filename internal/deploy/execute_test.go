package deploy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteUpdatesVersionIDAfterRouterBackendUpdate(t *testing.T) {
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\n")

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == sourceSnapshotsEndpoint:
			writeJSON(t, w, map[string]any{
				"source_snapshot_id": "src_123",
				"upload_url":         server.URL + "/upload",
				"upload_headers":     map[string]string{},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/upload":
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == finalizeEndpoint("src_123"):
			writeJSON(t, w, map[string]any{"status": "ready"})
		case r.Method == http.MethodGet && r.URL.Path == manifestEndpoint("src_123"):
			writeJSON(t, w, map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			writeJSON(t, w, map[string]any{"agent_id": "agt_123", "version_id": "ver_initial"})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/agents/agt_123/model-backend":
			writeJSON(t, w, map[string]any{"active_version_id": "ver_router"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var events []string
	response, err := Execute(context.Background(), dir, Config{
		APIURL:   server.URL,
		APIKey:   "key_123",
		RouterID: "rtr_123",
		Progress: func(event string, _ map[string]any) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response["version_id"] != "ver_router" {
		t.Fatalf("version_id = %v, want ver_router", response["version_id"])
	}
	if response["active_version_id"] != "ver_router" {
		t.Fatalf("active_version_id = %v, want ver_router", response["active_version_id"])
	}
	assertEventOrder(t, events, "publish:complete", "model_backend:start")
}

func TestExecuteUsesTargetAgentIDForRouterBackendWhenPublishOmitsAgentID(t *testing.T) {
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\n")

	var sawModelBackend bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == sourceSnapshotsEndpoint:
			writeJSON(t, w, map[string]any{
				"source_snapshot_id": "src_123",
				"upload_url":         server.URL + "/upload",
				"upload_headers":     map[string]string{},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/upload":
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == finalizeEndpoint("src_123"):
			writeJSON(t, w, map[string]any{"status": "ready"})
		case r.Method == http.MethodGet && r.URL.Path == manifestEndpoint("src_123"):
			writeJSON(t, w, map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents/agt_existing/versions":
			writeJSON(t, w, map[string]any{"version_id": "ver_initial"})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/agents/agt_existing/model-backend":
			sawModelBackend = true
			writeJSON(t, w, map[string]any{"active_version_id": "ver_router"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	response, err := Execute(context.Background(), dir, Config{
		APIURL:   server.URL,
		APIKey:   "key_123",
		AgentID:  "agt_existing",
		RouterID: "rtr_123",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !sawModelBackend {
		t.Fatal("model backend update was not called")
	}
	if response["version_id"] != "ver_router" {
		t.Fatalf("version_id = %v, want ver_router", response["version_id"])
	}
}

func TestExecutePersistsDeployStateBeforeRouterBackendUpdate(t *testing.T) {
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\n")

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == sourceSnapshotsEndpoint:
			writeJSON(t, w, map[string]any{
				"source_snapshot_id": "src_123",
				"upload_url":         server.URL + "/upload",
				"upload_headers":     map[string]string{},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/upload":
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == finalizeEndpoint("src_123"):
			writeJSON(t, w, map[string]any{"status": "ready"})
		case r.Method == http.MethodGet && r.URL.Path == manifestEndpoint("src_123"):
			writeJSON(t, w, map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			writeJSON(t, w, map[string]any{"agent_id": "agt_123", "version_id": "ver_123"})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/agents/agt_123/model-backend":
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(t, w, map[string]any{"detail": "router backend failed"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	_, err := Execute(context.Background(), dir, Config{
		APIURL:   server.URL,
		APIKey:   "key_123",
		RouterID: "rtr_123",
	})
	if err == nil || !strings.Contains(err.Error(), "set model backend") {
		t.Fatalf("Execute error = %v, want set model backend failure", err)
	}

	agentID, err := readDeployState(dir, server.URL)
	if err != nil {
		t.Fatalf("readDeployState: %v", err)
	}
	if agentID != "agt_123" {
		t.Fatalf("deploy state agent ID = %q, want agt_123", agentID)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func assertEventOrder(t *testing.T, events []string, before string, after string) {
	t.Helper()
	beforeIndex := -1
	afterIndex := -1
	for i, event := range events {
		if event == before && beforeIndex == -1 {
			beforeIndex = i
		}
		if event == after && afterIndex == -1 {
			afterIndex = i
		}
	}
	if beforeIndex == -1 {
		t.Fatalf("event %q not found in %v", before, events)
	}
	if afterIndex == -1 {
		t.Fatalf("event %q not found in %v", after, events)
	}
	if beforeIndex > afterIndex {
		t.Fatalf("event %q at index %d should run before %q at index %d; events=%v", before, beforeIndex, after, afterIndex, events)
	}
}
