// Package deploy orchestrates the 6-stage `dari deploy` flow: package →
// reserve → upload → finalize → validate → publish. Progress is emitted as
// `{stage}:{start|complete}` events to a callback the CLI turns into stderr
// output.
package deploy

import "github.com/mupt-ai/dari-cli/internal/bundle"

const (
	sourceSnapshotsEndpoint = "/v1/source-snapshots"

	// Placeholders used in --dry-run output so scripted consumers know
	// which fields resolve only after the reserve call.
	sourceSnapshotIDPlaceholder = "<source_snapshot_id from reserve step>"
	signedUploadURLPlaceholder  = "<signed upload URL from reserve step>"
	uploadHeadersPlaceholder    = "<upload_headers from reserve step>"
)

// PrepareOptions controls optional publish-time deploy behavior.
type PrepareOptions struct {
	AgentID  string
	RouterID string
}

// PreparedFlow is what `dari deploy --dry-run` prints.
type PreparedFlow struct {
	Bundle          *bundle.Archive
	BundleMetadata  bundle.Metadata
	PublishEndpoint string
	AgentID         string
	RouterID        string
	IsNewAgent      bool
}

// BuildPublishEndpoint returns the publish URL for deploy-by-name or an
// explicitly targeted existing agent.
func BuildPublishEndpoint(agentID string) string {
	if agentID == "" {
		return "/v1/agents"
	}
	return "/v1/agents/" + agentID + "/versions"
}

// finalizeEndpoint returns the POST endpoint that seals a reserved snapshot.
func finalizeEndpoint(snapshotID string) string {
	return sourceSnapshotsEndpoint + "/" + snapshotID + "/finalize"
}

// manifestEndpoint returns the GET endpoint that validates a finalized
// snapshot's dari.yml.
func manifestEndpoint(snapshotID string) string {
	return sourceSnapshotsEndpoint + "/" + snapshotID + "/manifest"
}

// deleteSnapshotEndpoint returns the DELETE endpoint for cleanup.
func deleteSnapshotEndpoint(snapshotID string) string {
	return sourceSnapshotsEndpoint + "/" + snapshotID
}

// Prepare builds the local deploy flow (bundle + publish endpoint) without
// making any network calls. Used by `--dry-run` and as the first step of a
// live deploy.
func Prepare(deployRoot, apiURL, agentID string) (*PreparedFlow, error) {
	return PrepareWithOptions(deployRoot, apiURL, PrepareOptions{AgentID: agentID})
}

// PrepareWithOptions builds the local deploy flow with optional publish-time
// overrides such as the agent model backend.
func PrepareWithOptions(
	deployRoot, apiURL string,
	options PrepareOptions,
) (*PreparedFlow, error) {
	resolvedAgentID := options.AgentID
	if resolvedAgentID == "" && apiURL != "" {
		// Remember the last-published agent ID for this API URL.
		id, err := readDeployState(deployRoot, apiURL)
		if err != nil {
			return nil, err
		}
		resolvedAgentID = id
	}

	normalizedRouterID, err := NormalizeRouterID(options.RouterID)
	if err != nil {
		return nil, err
	}

	metadata := bundle.CollectMetadata(deployRoot)
	archive, err := bundle.Build(deployRoot)
	if err != nil {
		return nil, err
	}
	return &PreparedFlow{
		Bundle:          archive,
		BundleMetadata:  metadata,
		PublishEndpoint: BuildPublishEndpoint(resolvedAgentID),
		AgentID:         resolvedAgentID,
		RouterID:        normalizedRouterID,
		IsNewAgent:      resolvedAgentID == "",
	}, nil
}

// DryRunPayload renders a PreparedFlow as the JSON document `dari deploy
// --dry-run` prints.
func (p *PreparedFlow) DryRunPayload() map[string]any {
	action := "publish_agent_version"
	if p.IsNewAgent {
		action = "create_or_version_agent_by_name"
	}
	return map[string]any{
		"bundle": map[string]any{
			"file_count": p.Bundle.FileCount(),
			"sha256":     p.Bundle.SHA256,
			"size_bytes": p.Bundle.SizeBytes(),
			"metadata":   nilIfEmpty(p.BundleMetadata),
		},
		"steps": []any{
			map[string]any{
				"action":   "reserve_source_snapshot",
				"method":   "POST",
				"endpoint": sourceSnapshotsEndpoint,
				"payload":  p.reservationPayload(),
			},
			map[string]any{
				"action":     "upload_source_bundle",
				"method":     "PUT",
				"upload_url": signedUploadURLPlaceholder,
				"headers":    uploadHeadersPlaceholder,
				"size_bytes": p.Bundle.SizeBytes(),
				"sha256":     p.Bundle.SHA256,
			},
			map[string]any{
				"action":   "finalize_source_snapshot",
				"method":   "POST",
				"endpoint": finalizeEndpoint(sourceSnapshotIDPlaceholder),
			},
			map[string]any{
				"action":   "validate_source_snapshot_manifest",
				"method":   "GET",
				"endpoint": manifestEndpoint(sourceSnapshotIDPlaceholder),
			},
			map[string]any{
				"action":   action,
				"method":   "POST",
				"endpoint": p.PublishEndpoint,
				"payload":  p.publishPayload(sourceSnapshotIDPlaceholder),
			},
		},
	}
}

func (p *PreparedFlow) publishPayload(sourceSnapshotID string) map[string]any {
	payload := map[string]any{"source_snapshot_id": sourceSnapshotID}
	if p.RouterID != "" {
		payload["runtime_metadata"] = map[string]any{
			"agent_host": map[string]any{
				"model_backend": map[string]any{
					"kind":      "router",
					"router_id": p.RouterID,
				},
			},
		}
	}
	return payload
}

func (p *PreparedFlow) reservationPayload() map[string]any {
	out := map[string]any{
		"format":     "tar.gz",
		"sha256":     p.Bundle.SHA256,
		"size_bytes": p.Bundle.SizeBytes(),
	}
	if len(p.BundleMetadata) > 0 {
		out["metadata"] = map[string]any(p.BundleMetadata)
	}
	return out
}

func nilIfEmpty(m bundle.Metadata) any {
	if len(m) == 0 {
		return nil
	}
	return map[string]any(m)
}
