package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDryRunPayloadOmitsRuntimeMetadataWithRouter(t *testing.T) {
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\n")

	prepared, err := PrepareWithOptions(dir, "https://api.example", PrepareOptions{
		RouterID: " rtr_123 ",
	})
	if err != nil {
		t.Fatalf("PrepareWithOptions: %v", err)
	}

	payload := prepared.DryRunPayload()
	steps := payload["steps"].([]any)
	publishStep := steps[len(steps)-2].(map[string]any)
	publishPayload := publishStep["payload"].(map[string]any)
	if _, ok := publishPayload["runtime_metadata"]; ok {
		t.Fatalf("runtime_metadata should be omitted from publish payload")
	}
	modelBackendStep := steps[len(steps)-1].(map[string]any)
	modelBackendPayload := modelBackendStep["payload"].(map[string]any)
	if modelBackendStep["action"] != "set_model_backend" {
		t.Fatalf("router dry-run final action = %v, want set_model_backend", modelBackendStep["action"])
	}
	if modelBackendPayload["router_id"] != "rtr_123" {
		t.Fatalf("router dry-run router_id = %v, want rtr_123", modelBackendPayload["router_id"])
	}
	if prepared.RouterID != "rtr_123" {
		t.Fatalf("RouterID = %q, want rtr_123", prepared.RouterID)
	}
}

func TestDryRunPayloadOmitsRuntimeMetadataWithoutRouter(t *testing.T) {
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\n")

	prepared, err := PrepareWithOptions(dir, "https://api.example", PrepareOptions{})
	if err != nil {
		t.Fatalf("PrepareWithOptions: %v", err)
	}

	payload := prepared.DryRunPayload()
	steps := payload["steps"].([]any)
	publishStep := steps[len(steps)-1].(map[string]any)
	publishPayload := publishStep["payload"].(map[string]any)

	if _, ok := publishPayload["runtime_metadata"]; ok {
		t.Fatalf("runtime_metadata should be omitted")
	}
	if got := publishPayload["source_snapshot_id"]; got != sourceSnapshotIDPlaceholder {
		t.Fatalf("source_snapshot_id = %v, want placeholder", got)
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
