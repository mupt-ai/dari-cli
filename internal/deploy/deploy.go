package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mupt-ai/dari-cli/internal/api"
)

// deployRequestTimeout is the per-request timeout for deploy-path HTTP
// calls. The publish step (POST /v1/agents[/...]/versions) can take tens
// of seconds server-side because it validates + provisions, so we use a
// much longer ceiling than api.DefaultTimeout (15s). Upload uses its
// own internal ceiling.
const deployRequestTimeout = 5 * time.Minute

// Progress receives event/data tuples describing deploy stage transitions.
// Event names are "{stage}:start" or "{stage}:complete" for stages in order:
// package, reserve, upload, finalize, validate, publish.
type Progress func(event string, data map[string]any)

// Config holds the per-deploy inputs. The caller owns the API URL and key
// resolution (flag → env → cached managed key).
type Config struct {
	APIURL   string
	APIKey   string
	AgentID  string // optional; empty means create a new agent
	Progress Progress
}

type reserveResponse struct {
	SourceSnapshotID string            `json:"source_snapshot_id"`
	UploadURL        string            `json:"upload_url"`
	UploadHeaders    map[string]string `json:"upload_headers"`
}

type finalizeResponse struct {
	Status        string `json:"status"`
	FailureReason string `json:"failure_reason"`
}

// Execute runs the full deploy flow and returns the publish response.
func Execute(ctx context.Context, deployRoot string, cfg Config) (map[string]any, error) {
	emit := func(event string, data map[string]any) {
		if cfg.Progress != nil {
			cfg.Progress(event, data)
		}
	}

	emit("package:start", nil)
	prepared, err := Prepare(deployRoot, cfg.APIURL, cfg.AgentID)
	if err != nil {
		return nil, err
	}
	emit("package:complete", map[string]any{
		"size_bytes": prepared.Bundle.SizeBytes(),
		"file_count": prepared.Bundle.FileCount(),
		"sha256":     prepared.Bundle.SHA256,
	})

	client := api.New(cfg.APIURL).WithBearer(cfg.APIKey)
	client.HTTP = &http.Client{Timeout: deployRequestTimeout}

	emit("reserve:start", nil)
	var reserve reserveResponse
	if err := client.Do(ctx, http.MethodPost, sourceSnapshotsEndpoint, prepared.reservationPayload(), &reserve); err != nil {
		return nil, deployAPIError("reserve source snapshot", err)
	}
	if reserve.SourceSnapshotID == "" || reserve.UploadURL == "" {
		return nil, errors.New("source snapshot reserve response missing required fields")
	}
	emit("reserve:complete", map[string]any{"source_snapshot_id": reserve.SourceSnapshotID})

	emit("upload:start", map[string]any{"size_bytes": prepared.Bundle.SizeBytes()})
	if err := client.Upload(ctx, reserve.UploadURL, prepared.Bundle.Content, reserve.UploadHeaders); err != nil {
		return nil, fmt.Errorf("bundle upload failed: %w", err)
	}
	emit("upload:complete", map[string]any{"size_bytes": prepared.Bundle.SizeBytes()})

	emit("finalize:start", nil)
	var finalize finalizeResponse
	if err := client.Do(ctx, http.MethodPost, finalizeEndpoint(reserve.SourceSnapshotID), nil, &finalize); err != nil {
		return nil, deployAPIError("finalize source snapshot", err)
	}
	if finalize.Status != "ready" {
		detail := finalize.FailureReason
		if strings.TrimSpace(detail) == "" {
			detail = "Source snapshot failed verification."
		}
		return nil, fmt.Errorf("source snapshot %s did not become ready: %s", reserve.SourceSnapshotID, detail)
	}
	emit("finalize:complete", map[string]any{"source_snapshot_id": reserve.SourceSnapshotID})

	emit("validate:start", nil)
	if err := client.Do(ctx, http.MethodGet, manifestEndpoint(reserve.SourceSnapshotID), nil, nil); err != nil {
		cleanupErr := deleteSnapshotBestEffort(ctx, client, reserve.SourceSnapshotID)
		if cleanupErr != nil {
			return nil, fmt.Errorf("uploaded bundle failed manifest validation and cleanup also failed for snapshot %s: %w; cleanup error: %v",
				reserve.SourceSnapshotID, err, cleanupErr)
		}
		return nil, deployAPIError("validate manifest", err)
	}
	emit("validate:complete", nil)

	emit("publish:start", map[string]any{"is_new_agent": prepared.IsNewAgent})
	var response map[string]any
	if err := client.Do(ctx, http.MethodPost, prepared.PublishEndpoint,
		map[string]any{"source_snapshot_id": reserve.SourceSnapshotID}, &response); err != nil {
		cleanupErr := deleteSnapshotBestEffort(ctx, client, reserve.SourceSnapshotID)
		if cleanupErr != nil {
			return nil, fmt.Errorf("publish failed after snapshot finalize and cleanup also failed for snapshot %s: %w; cleanup error: %v",
				reserve.SourceSnapshotID, err, cleanupErr)
		}
		return nil, deployAPIError("publish agent", err)
	}

	agentID, _ := response["agent_id"].(string)
	if agentID == "" {
		agentID, _ = response["id"].(string)
	}
	if agentID != "" {
		if werr := writeDeployState(deployRoot, cfg.APIURL, agentID); werr != nil {
			// Non-fatal — publish already succeeded.
			emit("publish:warn", map[string]any{"warning": "failed to persist .dari/deploy-state.json", "error": werr.Error()})
		}
	}
	emit("publish:complete", map[string]any{"agent_id": agentID})
	return response, nil
}

func deleteSnapshotBestEffort(ctx context.Context, client *api.Client, snapshotID string) error {
	return client.Do(ctx, http.MethodDelete, deleteSnapshotEndpoint(snapshotID), nil, nil)
}

// deployAPIError formats an API error with a context label. HTTP errors keep
// their server-supplied detail so the user can tell what went wrong.
func deployAPIError(context string, err error) error {
	if he := api.AsHTTPError(err); he != nil {
		return fmt.Errorf("Dari API request failed for %s with %d: %s", context, he.Status, he.Detail)
	}
	return fmt.Errorf("Dari API request failed for %s: %w", context, err)
}
