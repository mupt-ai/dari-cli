package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/mupt-ai/dari-cli/internal/deploy"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{
			Use:     "router",
			Aliases: []string{"routers"},
			Short:   "Manage Dari Routers for the current org",
		}
		cmd.AddCommand(
			newRouterListCmd(gf),
			newRouterGetCmd(gf),
			newRouterModelsCmd(gf),
			newRouterCreateCmd(gf),
			newRouterUpdateCmd(gf),
			newRouterDeleteCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newRouterListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List routers for the current org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/organizations/current/routers", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newRouterGetCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <router_id_or_endpoint>",
		Short: "Show one router for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			routerID, err := deploy.NormalizeRouterID(args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/organizations/current/routers/"+url.PathEscape(routerID), nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newRouterModelsCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List models available for router selection, grouped by provider",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/organizations/current/routers/model-catalog", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

type routerCustomRule struct {
	When          string  `json:"when" yaml:"when"`
	Use           string  `json:"use" yaml:"use"`
	ThinkingLevel *string `json:"thinking_level,omitempty" yaml:"thinking_level"`
}

type routerCustomConfig struct {
	Rules                []routerCustomRule `json:"rules" yaml:"rules"`
	Default              *string            `json:"default,omitempty" yaml:"default"`
	DefaultThinkingLevel *string            `json:"default_thinking_level,omitempty" yaml:"default_thinking_level"`
}

type routerCreateRequest struct {
	Name                string              `json:"name"`
	EnabledModels       []string            `json:"enabled_models"`
	ProviderKeys        map[string]string   `json:"provider_keys,omitempty"`
	ProviderKeySources  map[string]string   `json:"provider_key_sources,omitempty"`
	EvalIDs             []string            `json:"eval_ids,omitempty"`
	RoutingStrategy     string              `json:"routing_strategy,omitempty"`
	CustomConfig        *routerCustomConfig `json:"custom_config,omitempty"`
	ModelThinkingLevels map[string][]string `json:"model_thinking_levels,omitempty"`
	FastModels          []string            `json:"fast_models,omitempty"`
}

type routerUpdateRequest struct {
	Name               string            `json:"name"`
	EnabledModels      []string          `json:"enabled_models"`
	ProviderKeys       map[string]string `json:"provider_keys,omitempty"`
	ProviderKeySources map[string]string `json:"provider_key_sources,omitempty"`
	EvalIDs            *[]string         `json:"eval_ids,omitempty"`
	RoutingStrategy    string            `json:"routing_strategy,omitempty"`
}

type routerCurrent struct {
	Name            string   `json:"name"`
	EnabledModels   []string `json:"enabled_models"`
	RoutingStrategy string   `json:"routing_strategy"`
}

type routerCreateManifest struct {
	Name                string              `yaml:"name"`
	EnabledModels       []string            `yaml:"enabled_models"`
	ProviderKeys        map[string]string   `yaml:"provider_keys"`
	ProviderKeyEnvs     map[string]string   `yaml:"provider_key_envs"`
	ProviderKeySources  map[string]string   `yaml:"provider_key_sources"`
	EvalIDs             []string            `yaml:"eval_ids"`
	RoutingStrategy     string              `yaml:"routing_strategy"`
	CustomConfig        *routerCustomConfig `yaml:"custom_config"`
	ModelThinkingLevels map[string][]string `yaml:"model_thinking_levels"`
	FastModels          []string            `yaml:"fast_models"`
}

type routerConfigFlags struct {
	models             []string
	providerKeyPairs   []string
	providerKeyEnvs    []string
	managedKeyProvider []string
	evalIDs            []string
	clearEvals         bool
	strategy           string
	flagNames          []string
}

func (rf *routerConfigFlags) register(cmd *cobra.Command) {
	// name records every registered flag so changed() below cannot drift
	// from the set of flags this struct owns.
	name := func(flagName string) string {
		rf.flagNames = append(rf.flagNames, flagName)
		return flagName
	}
	cmd.Flags().StringSliceVar(&rf.models, name("model"), nil, "Enabled model ID (repeatable or comma-separated); run 'dari router models' for the catalog")
	cmd.Flags().StringArrayVar(&rf.providerKeyPairs, name("provider-key"), nil, "Provider API key as provider=KEY, e.g. fireworks=sk-... (repeatable)")
	cmd.Flags().StringArrayVar(&rf.providerKeyEnvs, name("provider-key-env"), nil, "Read a provider API key from the local environment as provider=ENV_VAR (repeatable)")
	cmd.Flags().StringSliceVar(&rf.managedKeyProvider, name("managed-key"), nil, "Use the Dari-managed key for this provider (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&rf.evalIDs, name("eval"), nil, "Eval scorecard ID to import (repeatable or comma-separated); run 'dari eval list' for IDs")
	cmd.Flags().StringVar(&rf.strategy, name("strategy"), "", "Routing strategy: slm; use a manifest for custom rules")
}

func (rf *routerConfigFlags) changed(cmd *cobra.Command) bool {
	return slices.ContainsFunc(rf.flagNames, cmd.Flags().Changed)
}

func (rf *routerConfigFlags) providerKeys(stderr io.Writer) (map[string]string, error) {
	keys := map[string]string{}
	for _, pair := range rf.providerKeyPairs {
		provider, value, err := splitPair(pair, "--provider-key", "provider=KEY")
		if err != nil {
			return nil, err
		}
		fmt.Fprintln(stderr, "Warning: passing provider keys on the command line can expose them via shell history and process arguments; prefer --provider-key-env.")
		keys[provider] = value
	}
	for _, pair := range rf.providerKeyEnvs {
		provider, envName, err := splitPair(pair, "--provider-key-env", "provider=ENV_VAR")
		if err != nil {
			return nil, err
		}
		value := os.Getenv(envName)
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("--provider-key-env %s: environment variable %s is empty or unset", pair, envName)
		}
		keys[provider] = value
	}
	return keys, nil
}

func (rf *routerConfigFlags) providerKeySources() map[string]string {
	if len(rf.managedKeyProvider) == 0 {
		return nil
	}
	sources := map[string]string{}
	for _, provider := range rf.managedKeyProvider {
		sources[strings.TrimSpace(provider)] = "managed"
	}
	return sources
}

func newRouterCreateCmd(gf *globalFlags) *cobra.Command {
	rf := &routerConfigFlags{}
	var manifestPath string
	cmd := &cobra.Command{
		Use:   "create <name_or_manifest_path>",
		Short: "Create a router for the current org",
		Args: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("from-file") {
				if len(args) > 0 {
					return fmt.Errorf("--from-file cannot be combined with a positional name or manifest path")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("from-file") {
				if rf.changed(cmd) {
					return fmt.Errorf("--from-file cannot be combined with router config flags; put the configuration in the manifest")
				}
				return createRouterFromManifest(cmd, gf, manifestPath)
			}
			if !rf.changed(cmd) && looksLikeRouterManifestPath(args[0]) {
				return createRouterFromManifest(cmd, gf, args[0])
			}
			if len(rf.models) == 0 {
				return fmt.Errorf("at least one --model is required; run 'dari router models' for the catalog")
			}
			body := routerCreateRequest{
				Name:          strings.TrimSpace(args[0]),
				EnabledModels: rf.models,
			}
			keys, err := rf.providerKeys(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if len(keys) > 0 {
				body.ProviderKeys = keys
			}
			if sources := rf.providerKeySources(); sources != nil {
				body.ProviderKeySources = sources
			}
			if len(rf.evalIDs) > 0 {
				body.EvalIDs = rf.evalIDs
			}
			strategy, err := routerStrategyForCreate(rf.strategy)
			if err != nil {
				return err
			}
			if strategy != "" {
				body.RoutingStrategy = strategy
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost, "/v1/organizations/current/routers", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	rf.register(cmd)
	cmd.Flags().StringVarP(&manifestPath, "from-file", "f", "", "Create from a local router.yml/router.yaml file or a directory containing one")
	return cmd
}

func createRouterFromManifest(cmd *cobra.Command, gf *globalFlags, path string) error {
	body, err := loadRouterCreateRequestFromManifest(path)
	if err != nil {
		return err
	}
	var resp map[string]any
	if err := orgKeyRequest(cmd, gf, http.MethodPost, "/v1/organizations/current/routers", body, &resp); err != nil {
		return err
	}
	return printJSON(resp)
}

func newRouterUpdateCmd(gf *globalFlags) *cobra.Command {
	rf := &routerConfigFlags{}
	var name string
	cmd := &cobra.Command{
		Use:   "update <router_id_or_endpoint>",
		Short: "Update a router; only the flags you pass change",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			routerID, err := deploy.NormalizeRouterID(args[0])
			if err != nil {
				return err
			}
			var current routerCurrent
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/organizations/current/routers/"+url.PathEscape(routerID), nil, &current); err != nil {
				return err
			}
			body := routerUpdateRequest{
				Name:          current.Name,
				EnabledModels: current.EnabledModels,
			}
			if cmd.Flags().Changed("name") {
				body.Name = strings.TrimSpace(name)
			}
			if cmd.Flags().Changed("model") {
				body.EnabledModels = rf.models
			}
			keys, err := rf.providerKeys(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if len(keys) > 0 {
				body.ProviderKeys = keys
			}
			if sources := rf.providerKeySources(); sources != nil {
				body.ProviderKeySources = sources
			}
			switch {
			case rf.clearEvals:
				targetEvalIDs := []string{}
				body.EvalIDs = &targetEvalIDs
			case cmd.Flags().Changed("eval"):
				body.EvalIDs = &rf.evalIDs
			}
			strategy, err := routerStrategyForUpdate(rf.strategy, current.RoutingStrategy)
			if err != nil {
				return err
			}
			if rf.strategy != "" {
				body.RoutingStrategy = strategy
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPut, "/v1/organizations/current/routers/"+url.PathEscape(routerID), body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	rf.register(cmd)
	cmd.Flags().StringVar(&name, "name", "", "Rename the router")
	cmd.Flags().BoolVar(&rf.clearEvals, "clear-evals", false, "Remove all imported eval scorecards")
	return cmd
}

func newRouterDeleteCmd(gf *globalFlags) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <router_id_or_endpoint>",
		Short: "Soft-delete a router for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			routerID, err := deploy.NormalizeRouterID(args[0])
			if err != nil {
				return err
			}
			if !yes && !confirm(fmt.Sprintf("Delete router %s? Agents and API keys pointing at it will stop routing. [y/N] ", routerID)) {
				return fmt.Errorf("aborted")
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodDelete, "/v1/organizations/current/routers/"+url.PathEscape(routerID), nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the interactive confirmation prompt")
	return cmd
}

func routerStrategyForCreate(strategy string) (string, error) {
	switch strategy {
	case "", "slm":
		return strategy, nil
	case "custom":
		return "", fmt.Errorf("custom routing requires a manifest with custom_config; use dari router create --from-file")
	default:
		return "", fmt.Errorf("--strategy must be slm")
	}
}

func routerStrategyForUpdate(strategy, currentStrategy string) (string, error) {
	if strategy == "" {
		return currentStrategy, nil
	}
	if strategy == "custom" && currentStrategy != "custom" {
		return "", fmt.Errorf("custom routing rules cannot be set with flags; create the router from a manifest with custom_config (--from-file)")
	}
	if strategy != "slm" && strategy != "custom" {
		return "", fmt.Errorf("--strategy must be slm")
	}
	return strategy, nil
}

func splitPair(pair, flagName, format string) (string, string, error) {
	key, value, ok := strings.Cut(pair, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" || value == "" {
		return "", "", fmt.Errorf("invalid %s %q: expected %s", flagName, pair, format)
	}
	return key, value, nil
}

func looksLikeRouterManifestPath(arg string) bool {
	trimmed := strings.TrimSpace(arg)
	ext := strings.ToLower(filepath.Ext(trimmed))
	return trimmed == "." ||
		trimmed == ".." ||
		ext == ".yml" ||
		ext == ".yaml" ||
		strings.Contains(trimmed, "/") ||
		strings.Contains(trimmed, string(filepath.Separator))
}

func resolveRouterManifestPath(rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("router manifest path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("router manifest %s: %w", path, err)
	}
	if !info.IsDir() {
		return path, nil
	}
	for _, name := range []string{"router.yml", "router.yaml"} {
		candidate := filepath.Join(path, name)
		if candidateInfo, err := os.Stat(candidate); err == nil && !candidateInfo.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("router manifest directory %s must contain router.yml or router.yaml", path)
}

func loadRouterCreateRequestFromManifest(rawPath string) (routerCreateRequest, error) {
	path, err := resolveRouterManifestPath(rawPath)
	if err != nil {
		return routerCreateRequest{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return routerCreateRequest{}, fmt.Errorf("read router manifest %s: %w", path, err)
	}
	var manifest routerCreateManifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	// io.EOF means an empty manifest; let createRequest report the missing
	// fields instead of surfacing a bare "EOF".
	if err := decoder.Decode(&manifest); err != nil && !errors.Is(err, io.EOF) {
		return routerCreateRequest{}, fmt.Errorf("parse router manifest %s: %w", path, err)
	}
	return manifest.createRequest(path)
}

func (manifest routerCreateManifest) createRequest(path string) (routerCreateRequest, error) {
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		return routerCreateRequest{}, fmt.Errorf("%s: name is required", path)
	}
	models, err := cleanRequiredStrings(manifest.EnabledModels, path, "enabled_models")
	if err != nil {
		return routerCreateRequest{}, err
	}
	if len(models) == 0 {
		return routerCreateRequest{}, fmt.Errorf("%s: enabled_models must contain at least one model", path)
	}
	providerKeys, err := manifestProviderKeys(path, manifest.ProviderKeys, manifest.ProviderKeyEnvs)
	if err != nil {
		return routerCreateRequest{}, err
	}
	providerKeySources, err := cleanProviderKeySources(path, manifest.ProviderKeySources)
	if err != nil {
		return routerCreateRequest{}, err
	}
	if err := validateManifestProviderKeys(path, providerKeySources, providerKeys, models); err != nil {
		return routerCreateRequest{}, err
	}
	modelThinkingLevels, err := normalizeManifestModelThinkingLevels(
		path,
		manifest.ModelThinkingLevels,
		models,
	)
	if err != nil {
		return routerCreateRequest{}, err
	}
	fastModels, err := normalizeManifestFastModels(path, manifest.FastModels, models)
	if err != nil {
		return routerCreateRequest{}, err
	}
	evalIDs, err := cleanRequiredStrings(manifest.EvalIDs, path, "eval_ids")
	if err != nil {
		return routerCreateRequest{}, err
	}
	strategy, err := routerStrategyForManifest(
		path,
		strings.ToLower(strings.TrimSpace(manifest.RoutingStrategy)),
		manifest.CustomConfig != nil,
	)
	if err != nil {
		return routerCreateRequest{}, err
	}
	customConfig, err := normalizeManifestCustomConfig(
		path,
		manifest.CustomConfig,
		models,
		modelThinkingLevels,
	)
	if err != nil {
		return routerCreateRequest{}, err
	}
	body := routerCreateRequest{
		Name:          name,
		EnabledModels: models,
	}
	if len(providerKeys) > 0 {
		body.ProviderKeys = providerKeys
	}
	if len(providerKeySources) > 0 {
		body.ProviderKeySources = providerKeySources
	}
	if len(evalIDs) > 0 {
		body.EvalIDs = evalIDs
	}
	if strategy != "" {
		body.RoutingStrategy = strategy
	}
	body.CustomConfig = customConfig
	body.ModelThinkingLevels = modelThinkingLevels
	body.FastModels = fastModels
	return body, nil
}

func cleanRequiredStrings(values []string, path, field string) ([]string, error) {
	cleaned := make([]string, 0, len(values))
	for index, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, fmt.Errorf("%s: %s[%d] must be non-empty", path, field, index)
		}
		cleaned = append(cleaned, trimmed)
	}
	return cleaned, nil
}

// The API matches provider names case-insensitively, so manifest provider
// names are normalized to lowercase before validation and submission.
func manifestProviderKeys(path string, rawKeys, rawEnvs map[string]string) (map[string]string, error) {
	keys := map[string]string{}
	for rawProvider, rawValue := range rawKeys {
		provider := strings.ToLower(strings.TrimSpace(rawProvider))
		value := strings.TrimSpace(rawValue)
		if provider == "" || value == "" {
			return nil, fmt.Errorf("%s: provider_keys entries must be provider: key", path)
		}
		if _, exists := keys[provider]; exists {
			return nil, fmt.Errorf("%s: provider_keys defines provider %s more than once", path, provider)
		}
		keys[provider] = value
	}
	for rawProvider, rawEnv := range rawEnvs {
		provider := strings.ToLower(strings.TrimSpace(rawProvider))
		envName := strings.TrimSpace(rawEnv)
		if provider == "" || envName == "" {
			return nil, fmt.Errorf("%s: provider_key_envs entries must be provider: ENV_VAR", path)
		}
		if _, exists := keys[provider]; exists {
			return nil, fmt.Errorf("%s: provider %s is defined more than once across provider_keys and provider_key_envs", path, provider)
		}
		value := os.Getenv(envName)
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s: provider_key_envs.%s references unset or empty environment variable %s", path, provider, envName)
		}
		keys[provider] = value
	}
	return keys, nil
}

func cleanProviderKeySources(path string, rawSources map[string]string) (map[string]string, error) {
	sources := map[string]string{}
	for rawProvider, rawSource := range rawSources {
		provider := strings.ToLower(strings.TrimSpace(rawProvider))
		source := strings.TrimSpace(rawSource)
		if provider == "" {
			return nil, fmt.Errorf("%s: provider_key_sources has an empty provider name", path)
		}
		source = strings.ToLower(source)
		if source != "managed" && source != "user" {
			return nil, fmt.Errorf("%s: provider_key_sources.%s must be managed or user", path, provider)
		}
		if _, exists := sources[provider]; exists {
			return nil, fmt.Errorf("%s: provider_key_sources defines provider %s more than once", path, provider)
		}
		sources[provider] = source
	}
	return sources, nil
}

func validateManifestProviderKeys(path string, sources, keys map[string]string, models []string) error {
	providers, err := providersForRouterModels(path, models)
	if err != nil {
		return err
	}
	expected := map[string]bool{}
	for _, provider := range providers {
		expected[provider] = true
	}
	for provider := range sources {
		if !expected[provider] {
			return fmt.Errorf("%s: provider_key_sources.%s does not match any enabled_models provider", path, provider)
		}
	}
	for provider := range keys {
		if !expected[provider] {
			return fmt.Errorf("%s: provider key for %s does not match any enabled_models provider", path, provider)
		}
	}
	for provider, source := range sources {
		if source == "managed" && strings.TrimSpace(keys[provider]) != "" {
			return fmt.Errorf("%s: provider_keys can only include providers marked as user; %s is managed", path, provider)
		}
	}
	for _, provider := range providers {
		if !manifestProviderUsesManifestCredentials(provider) {
			if strings.TrimSpace(keys[provider]) != "" {
				return fmt.Errorf("%s: provider key for %s is configured by the custom model credential", path, provider)
			}
			if sources[provider] == "managed" {
				return fmt.Errorf("%s: provider_key_sources.%s cannot be managed for a custom model provider", path, provider)
			}
			continue
		}
		switch sources[provider] {
		case "":
			return fmt.Errorf("%s: provider_key_sources.%s is required; set it to managed or user", path, provider)
		case "user":
			if strings.TrimSpace(keys[provider]) == "" {
				return fmt.Errorf("%s: provider_key_sources.%s is user, so provider_keys.%s or provider_key_envs.%s is required", path, provider, provider, provider)
			}
		}
	}
	return nil
}

// Manifest validation is intentionally offline. Built-in router providers need
// manifest-managed key selection, while org custom model providers resolve their
// credentials from the model catalog and may omit manifest provider key fields.
func manifestProviderUsesManifestCredentials(provider string) bool {
	switch provider {
	case "anthropic", "baseten", "fireworks", "openai":
		return true
	default:
		return false
	}
}

func providersForRouterModels(path string, models []string) ([]string, error) {
	seen := map[string]bool{}
	providers := []string{}
	for _, model := range models {
		provider, _, ok := strings.Cut(model, "/")
		provider = strings.ToLower(provider)
		if !ok || provider == "" {
			return nil, fmt.Errorf("%s: enabled_models entry %s must be a provider-prefixed model ID like openai/gpt-5.5", path, model)
		}
		if seen[provider] {
			continue
		}
		seen[provider] = true
		providers = append(providers, provider)
	}
	return providers, nil
}

const (
	manifestCustomRulesMaxCount     = 20
	manifestCustomRuleWhenMaxLength = 500
)

var routerThinkingLevels = []string{
	"off",
	"minimal",
	"low",
	"medium",
	"high",
	"xhigh",
	"max",
}

func normalizeManifestModelThinkingLevels(
	path string,
	raw map[string][]string,
	models []string,
) (map[string][]string, error) {
	if raw == nil {
		return nil, nil
	}
	enabled := make(map[string]bool, len(models))
	for _, model := range models {
		enabled[model] = true
	}
	for model := range raw {
		if !enabled[model] {
			return nil, fmt.Errorf("%s: model_thinking_levels.%s is not in enabled_models", path, model)
		}
	}

	normalized := make(map[string][]string, len(models))
	for _, model := range models {
		rawLevels, ok := raw[model]
		if !ok {
			return nil, fmt.Errorf("%s: model_thinking_levels must contain every enabled_models entry", path)
		}
		if len(rawLevels) == 0 {
			return nil, fmt.Errorf("%s: model_thinking_levels.%s must contain at least one level", path, model)
		}
		selected := make(map[string]bool, len(rawLevels))
		for index, rawLevel := range rawLevels {
			level := strings.ToLower(strings.TrimSpace(rawLevel))
			if !slices.Contains(routerThinkingLevels, level) {
				return nil, fmt.Errorf(
					"%s: model_thinking_levels.%s[%d] must be one of: %s",
					path,
					model,
					index,
					strings.Join(routerThinkingLevels, ", "),
				)
			}
			selected[level] = true
		}
		levels := make([]string, 0, len(selected))
		for _, level := range routerThinkingLevels {
			if selected[level] {
				levels = append(levels, level)
			}
		}
		normalized[model] = levels
	}
	return normalized, nil
}

func normalizeManifestFastModels(path string, raw, models []string) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	enabled := make(map[string]bool, len(models))
	for _, model := range models {
		enabled[model] = true
	}
	selected := make(map[string]bool, len(raw))
	for index, rawModel := range raw {
		model := strings.TrimSpace(rawModel)
		if model == "" {
			return nil, fmt.Errorf("%s: fast_models[%d] must be non-empty", path, index)
		}
		if !enabled[model] {
			return nil, fmt.Errorf("%s: fast_models[%d] must be one of enabled_models", path, index)
		}
		selected[model] = true
	}
	normalized := make([]string, 0, len(selected))
	for _, model := range models {
		if selected[model] {
			normalized = append(normalized, model)
		}
	}
	return normalized, nil
}

func normalizeManifestCustomConfig(
	path string,
	config *routerCustomConfig,
	models []string,
	modelThinkingLevels map[string][]string,
) (*routerCustomConfig, error) {
	if config == nil {
		return nil, nil
	}
	if len(config.Rules) == 0 {
		return nil, fmt.Errorf("%s: custom_config.rules must contain at least one rule", path)
	}
	if len(config.Rules) > manifestCustomRulesMaxCount {
		return nil, fmt.Errorf("%s: custom_config.rules supports at most %d rules", path, manifestCustomRulesMaxCount)
	}
	enabled := map[string]bool{}
	for _, model := range models {
		enabled[model] = true
	}
	rules := make([]routerCustomRule, 0, len(config.Rules))
	for index, rule := range config.Rules {
		when := strings.TrimSpace(rule.When)
		if when == "" {
			return nil, fmt.Errorf("%s: custom_config.rules[%d].when must be non-empty", path, index)
		}
		if len([]rune(when)) > manifestCustomRuleWhenMaxLength {
			return nil, fmt.Errorf("%s: custom_config.rules[%d].when must be at most %d characters", path, index, manifestCustomRuleWhenMaxLength)
		}
		use := strings.TrimSpace(rule.Use)
		if use == "" {
			return nil, fmt.Errorf("%s: custom_config.rules[%d].use must be non-empty", path, index)
		}
		if !enabled[use] {
			return nil, fmt.Errorf("%s: custom_config.rules[%d].use must be one of enabled_models", path, index)
		}
		thinkingLevel, err := normalizeManifestTargetThinkingLevel(
			fmt.Sprintf("%s: custom_config.rules[%d].thinking_level", path, index),
			use,
			rule.ThinkingLevel,
			modelThinkingLevels,
		)
		if err != nil {
			return nil, err
		}
		rules = append(rules, routerCustomRule{
			When:          when,
			Use:           use,
			ThinkingLevel: thinkingLevel,
		})
	}
	var defaultModel *string
	var defaultThinkingLevel *string
	if config.Default != nil {
		value := strings.TrimSpace(*config.Default)
		if value == "" {
			return nil, fmt.Errorf("%s: custom_config.default must be non-empty when set", path)
		}
		if !enabled[value] {
			return nil, fmt.Errorf("%s: custom_config.default must be one of enabled_models", path)
		}
		defaultModel = &value
		normalizedDefaultThinkingLevel, err := normalizeManifestTargetThinkingLevel(
			path+": custom_config.default_thinking_level",
			value,
			config.DefaultThinkingLevel,
			modelThinkingLevels,
		)
		if err != nil {
			return nil, err
		}
		defaultThinkingLevel = normalizedDefaultThinkingLevel
	} else if config.DefaultThinkingLevel != nil {
		return nil, fmt.Errorf("%s: custom_config.default is required when default_thinking_level is set", path)
	}
	return &routerCustomConfig{
		Rules:                rules,
		Default:              defaultModel,
		DefaultThinkingLevel: defaultThinkingLevel,
	}, nil
}

func normalizeManifestTargetThinkingLevel(
	label string,
	model string,
	raw *string,
	modelThinkingLevels map[string][]string,
) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	level := strings.ToLower(strings.TrimSpace(*raw))
	if !slices.Contains(routerThinkingLevels, level) {
		return nil, fmt.Errorf(
			"%s must be null or one of: %s",
			label,
			strings.Join(routerThinkingLevels, ", "),
		)
	}
	if enabledLevels := modelThinkingLevels[model]; enabledLevels != nil && !slices.Contains(enabledLevels, level) {
		return nil, fmt.Errorf("%s must be enabled in model_thinking_levels.%s", label, model)
	}
	return &level, nil
}

func routerStrategyForManifest(path, strategy string, hasCustomConfig bool) (string, error) {
	switch {
	case strategy == "custom" && !hasCustomConfig:
		return "", fmt.Errorf("%s: routing_strategy custom requires custom_config", path)
	case strategy == "slm" && hasCustomConfig:
		return "", fmt.Errorf("%s: custom_config is only supported when routing_strategy is custom", path)
	case strategy == "custom" || strategy == "slm":
		return strategy, nil
	case strategy != "":
		return "", fmt.Errorf("%s: routing_strategy must be slm or custom", path)
	case hasCustomConfig:
		return "custom", nil
	default:
		return "", nil
	}
}
