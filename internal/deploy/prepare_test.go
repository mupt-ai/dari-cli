package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDryRunPayloadOmitsRuntimeMetadata(t *testing.T) {
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
			name: "prepare with options",
			prepare: func(dir string) (*PreparedFlow, error) {
				return PrepareWithOptions(dir, "https://api.example", PrepareOptions{})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: test\n")

			prepared, err := tt.prepare(dir)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
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
