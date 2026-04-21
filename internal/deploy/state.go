package deploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	deployStateDir     = ".dari"
	deployStateFile    = "deploy-state.json"
	deployStateVersion = 1
)

type deployStateOnDisk struct {
	Version int               `json:"version"`
	Agents  map[string]string `json:"agents"`
}

// readDeployState returns the cached agent ID for apiURL, or "" if none.
func readDeployState(repoRoot, apiURL string) (string, error) {
	m, err := loadDeployState(repoRoot)
	if err != nil {
		return "", err
	}
	return m[normalizeDeployAPIURL(apiURL)], nil
}

// writeDeployState caches agentID for apiURL.
func writeDeployState(repoRoot, apiURL, agentID string) error {
	if strings.TrimSpace(agentID) == "" {
		return nil
	}
	m, err := loadDeployState(repoRoot)
	if err != nil {
		return err
	}
	m[normalizeDeployAPIURL(apiURL)] = agentID

	path := deployStatePath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create .dari dir: %w", err)
	}
	data, err := json.MarshalIndent(deployStateOnDisk{
		Version: deployStateVersion,
		Agents:  m,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func loadDeployState(repoRoot string) (map[string]string, error) {
	path := deployStatePath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var raw deployStateOnDisk
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse deploy state %s: %w", path, err)
	}
	if raw.Version != deployStateVersion {
		return nil, fmt.Errorf("unsupported deploy state schema version in %s", path)
	}
	out := make(map[string]string, len(raw.Agents))
	for k, v := range raw.Agents {
		nk := normalizeDeployAPIURL(k)
		nv := strings.TrimSpace(v)
		if nk != "" && nv != "" {
			out[nk] = nv
		}
	}
	return out, nil
}

func deployStatePath(repoRoot string) string {
	return filepath.Join(repoRoot, deployStateDir, deployStateFile)
}

func normalizeDeployAPIURL(apiURL string) string {
	return strings.TrimRight(strings.TrimSpace(apiURL), "/")
}
