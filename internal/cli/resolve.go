package cli

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

type agentReferenceSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type sessionReferenceSummary struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
}

func resolveAgentRef(cmd *cobra.Command, gf *globalFlags, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("agent reference must be non-empty")
	}
	if strings.HasPrefix(ref, "agt_") {
		return ref, nil
	}

	agents, err := listAgentsForResolution(cmd, gf)
	if err != nil {
		return "", err
	}
	return matchAgentRef(ref, agents)
}

func matchAgentRef(ref string, agents []agentReferenceSummary) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("agent reference must be non-empty")
	}
	if strings.HasPrefix(ref, "agt_") {
		return ref, nil
	}
	for _, agent := range agents {
		if agent.ID == ref {
			return agent.ID, nil
		}
	}
	matches := make([]agentReferenceSummary, 0, 1)
	for _, agent := range agents {
		if agent.Name == ref {
			matches = append(matches, agent)
		}
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("agent name %q is ambiguous; matching agent IDs: %s", ref, joinAgentIDs(matches))
	}
	return "", fmt.Errorf("no agent named %q found; pass an agent ID", ref)
}

func resolveSessionRef(cmd *cobra.Command, gf *globalFlags, ref, agentRef string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("session reference must be non-empty")
	}
	if strings.HasPrefix(ref, "sess_") {
		return ref, nil
	}

	if strings.TrimSpace(agentRef) != "" {
		agentID, err := resolveAgentRef(cmd, gf, agentRef)
		if err != nil {
			return "", err
		}
		sessions, err := listSessionsForAgentResolution(cmd, gf, agentID)
		if err != nil {
			return "", err
		}
		return matchSessionRef(ref, sessions, fmt.Sprintf(" for agent %s", agentID))
	}

	agents, err := listAgentsForResolution(cmd, gf)
	if err != nil {
		return "", err
	}
	matches := make([]sessionReferenceSummary, 0, 1)
	for _, agent := range agents {
		sessions, err := listSessionsForAgentResolution(cmd, gf, agent.ID)
		if err != nil {
			return "", err
		}
		for _, session := range sessions {
			if session.ID == ref || (session.Name != nil && *session.Name == ref) {
				matches = append(matches, session)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("session name %q is ambiguous; matching session IDs: %s (pass --agent or a session ID)", ref, joinSessionIDs(matches))
	}
	return "", fmt.Errorf("no session named %q found; pass a session ID", ref)
}

func listAgentsForResolution(cmd *cobra.Command, gf *globalFlags) ([]agentReferenceSummary, error) {
	var resp struct {
		Agents []agentReferenceSummary `json:"agents"`
	}
	if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

func listSessionsForAgentResolution(cmd *cobra.Command, gf *globalFlags, agentID string) ([]sessionReferenceSummary, error) {
	var resp struct {
		Sessions []sessionReferenceSummary `json:"sessions"`
	}
	if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents/"+agentID+"/sessions", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

func matchSessionRef(ref string, sessions []sessionReferenceSummary, scope string) (string, error) {
	for _, session := range sessions {
		if session.ID == ref {
			return session.ID, nil
		}
	}
	matches := make([]sessionReferenceSummary, 0, 1)
	for _, session := range sessions {
		if session.Name != nil && *session.Name == ref {
			matches = append(matches, session)
		}
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("session name %q is ambiguous%s; matching session IDs: %s", ref, scope, joinSessionIDs(matches))
	}
	return "", fmt.Errorf("no session named %q found%s; pass a session ID", ref, scope)
}

func joinAgentIDs(agents []agentReferenceSummary) string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	return strings.Join(ids, ", ")
}

func joinSessionIDs(sessions []sessionReferenceSummary) string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}
	return strings.Join(ids, ", ")
}
