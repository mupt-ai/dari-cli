package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDryRunPayloadIncludesRouterRuntimeMetadata(t *testing.T) {
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\nharness: pi\n")

	prepared, err := PrepareWithOptions(dir, "https://api.example", PrepareOptions{
		RouterID: " rtr_123 ",
	})
	if err != nil {
		t.Fatalf("PrepareWithOptions: %v", err)
	}

	payload := prepared.DryRunPayload()
	steps := payload["steps"].([]any)
	publishStep := steps[len(steps)-1].(map[string]any)
	publishPayload := publishStep["payload"].(map[string]any)
	runtimeMetadata := publishPayload["runtime_metadata"].(map[string]any)
	agentHost := runtimeMetadata["agent_host"].(map[string]any)
	modelBackend := agentHost["model_backend"].(map[string]any)

	if got := modelBackend["kind"]; got != "router" {
		t.Fatalf("model_backend.kind = %v, want router", got)
	}
	if got := modelBackend["router_id"]; got != "rtr_123" {
		t.Fatalf("model_backend.router_id = %v, want rtr_123", got)
	}
}

func TestDryRunPayloadOmitsRuntimeMetadataWithoutRouter(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(string) (*PreparedFlow, error)
	}{
		{
			name: "default prepare",
			prepare: func(dir string) (*PreparedFlow, error) {
				return Prepare(dir, "https://api.example", "")
			},
		},
		{
			name: "blank router override",
			prepare: func(dir string) (*PreparedFlow, error) {
				return PrepareWithOptions(dir, "https://api.example", PrepareOptions{RouterID: "  "})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\nharness: pi\n")

			prepared, err := tt.prepare(dir)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}

			payload := prepared.DryRunPayload()
			steps := payload["steps"].([]any)
			publishStep := steps[len(steps)-1].(map[string]any)
			publishPayload := publishStep["payload"].(map[string]any)

			if _, ok := publishPayload["runtime_metadata"]; ok {
				t.Fatalf("runtime_metadata should be omitted without a router override")
			}
		})
	}
}

func writeDeployFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
