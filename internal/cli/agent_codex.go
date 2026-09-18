package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

func configureCodexCommand(command *agentCommand, routerID string) {
	command.args = insertBeforeDoubleDash(command.args, []string{
		"--config", `model="dari/routing"`,
		"--config", `model_provider="dari"`,
		"--config", `model_reasoning_summary="none"`,
		"--config", `model_providers.dari.name="Dari"`,
		"--config", `model_providers.dari.base_url="` + agentRoutingBaseURL(routerID) + `/v1"`,
		"--config", `model_providers.dari.env_key="DARI_ROUTING_API_KEY"`,
		"--config", `model_providers.dari.wire_api="responses"`,
	})
}

// Codex does not discover remote catalogs for env-key custom providers. Supply
// the authenticated router catalog explicitly so auxiliary agents do not pick
// bundled OpenAI models that the routing endpoint cannot serve.
func prepareCodexCatalog(ctx context.Context, command *agentCommand, key, baseURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return fmt.Errorf("create Codex catalog request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch Codex model catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch Codex model catalog: HTTP %d", resp.StatusCode)
	}
	const maxCatalogBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return fmt.Errorf("read Codex model catalog: %w", err)
	}
	if len(body) > maxCatalogBytes {
		return fmt.Errorf("Codex model catalog exceeds size limit")
	}
	var catalog struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		return fmt.Errorf("decode Codex model catalog: %w", err)
	}
	found := false
	disableWebSearch := false
	for _, model := range catalog.Models {
		var slug string
		if json.Unmarshal(model["slug"], &slug) == nil && slug == routingModel {
			// Older servers use null to indicate unavailable web search. Codex
			// requires a concrete web-search enum, so disable the tool separately.
			if string(model["web_search_tool_type"]) == "null" {
				model["web_search_tool_type"] = json.RawMessage(`"text"`)
				disableWebSearch = true
			}
			for _, field := range []string{"tool_mode", "multi_agent_version"} {
				if string(model[field]) == "null" {
					delete(model, field)
				}
			}
			// Also works against servers deployed before the reviewer override.
			model["auto_review_model_override"] = json.RawMessage(strconv.Quote(routingModel))
			found = true
		}
	}
	if !found {
		return fmt.Errorf("Codex model catalog is missing %s", routingModel)
	}
	body, err = json.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("encode Codex model catalog: %w", err)
	}
	file, err := os.CreateTemp("", "dari-codex-models-*.json")
	if err != nil {
		return fmt.Errorf("create Codex model catalog: %w", err)
	}
	path := file.Name()
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write Codex model catalog: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close Codex model catalog: %w", err)
	}
	command.args = insertBeforeDoubleDash(command.args, []string{"--config", "model_catalog_json=" + strconv.Quote(path)})
	if disableWebSearch {
		command.args = insertBeforeDoubleDash(command.args, []string{"--config", `web_search="disabled"`})
	}
	command.cleanup = func() { _ = os.Remove(path) }
	return nil
}
