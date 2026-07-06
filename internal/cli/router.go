package cli

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

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

type routerEval struct {
	ID string `json:"id"`
}

type routerHeuristicConfig struct {
	PerformanceWeight float64            `json:"performance_weight"`
	PriceWeight       float64            `json:"price_weight"`
	EvalWeights       map[string]float64 `json:"eval_weights"`
}

type routerCreateRequest struct {
	Name               string                 `json:"name"`
	EnabledModels      []string               `json:"enabled_models"`
	ProviderKeys       map[string]string      `json:"provider_keys,omitempty"`
	ProviderKeySources map[string]string      `json:"provider_key_sources,omitempty"`
	EvalIDs            []string               `json:"eval_ids,omitempty"`
	RoutingStrategy    string                 `json:"routing_strategy,omitempty"`
	HeuristicConfig    *routerHeuristicConfig `json:"heuristic_config,omitempty"`
}

type routerUpdateRequest struct {
	Name               string                 `json:"name"`
	EnabledModels      []string               `json:"enabled_models"`
	ProviderKeys       map[string]string      `json:"provider_keys,omitempty"`
	ProviderKeySources map[string]string      `json:"provider_key_sources,omitempty"`
	EvalIDs            *[]string              `json:"eval_ids,omitempty"`
	RoutingStrategy    string                 `json:"routing_strategy,omitempty"`
	HeuristicConfig    *routerHeuristicConfig `json:"heuristic_config,omitempty"`
}

type routerCurrent struct {
	Name            string                 `json:"name"`
	EnabledModels   []string               `json:"enabled_models"`
	Evals           []routerEval           `json:"evals"`
	RoutingStrategy string                 `json:"routing_strategy"`
	HeuristicConfig *routerHeuristicConfig `json:"heuristic_config"`
}

type routerConfigFlags struct {
	models             []string
	providerKeyPairs   []string
	providerKeyEnvs    []string
	managedKeyProvider []string
	evalIDs            []string
	clearEvals         bool
	strategy           string
	performanceWeight  float64
	priceWeight        float64
	evalWeightPairs    []string
}

func (rf *routerConfigFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&rf.models, "model", nil, "Enabled model ID (repeatable or comma-separated); run 'dari router models' for the catalog")
	cmd.Flags().StringArrayVar(&rf.providerKeyPairs, "provider-key", nil, "Provider API key as provider=KEY, e.g. fireworks=sk-... (repeatable)")
	cmd.Flags().StringArrayVar(&rf.providerKeyEnvs, "provider-key-env", nil, "Read a provider API key from the local environment as provider=ENV_VAR (repeatable)")
	cmd.Flags().StringSliceVar(&rf.managedKeyProvider, "managed-key", nil, "Use the Dari-managed key for this provider (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&rf.evalIDs, "eval", nil, "Eval scorecard ID to import (repeatable or comma-separated); run 'dari eval list' for IDs")
	cmd.Flags().StringVar(&rf.strategy, "strategy", "", "Routing strategy: slm or heuristic")
	cmd.Flags().Float64Var(&rf.performanceWeight, "performance-weight", 0, "Heuristic strategy: weight for model performance (0-1)")
	cmd.Flags().Float64Var(&rf.priceWeight, "price-weight", 0, "Heuristic strategy: weight for model price (0-1)")
	cmd.Flags().StringArrayVar(&rf.evalWeightPairs, "eval-weight", nil, "Heuristic strategy: per-eval weight as eval_id=WEIGHT (repeatable)")
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

func (rf *routerConfigFlags) heuristicConfig(cmd *cobra.Command, currentConfig *routerHeuristicConfig, evalIDs []string) (*routerHeuristicConfig, error) {
	changed := cmd.Flags().Changed("performance-weight") ||
		cmd.Flags().Changed("price-weight") ||
		cmd.Flags().Changed("eval-weight")
	if !changed {
		return nil, nil
	}
	if !cmd.Flags().Changed("performance-weight") || !cmd.Flags().Changed("price-weight") {
		return nil, fmt.Errorf("heuristic config requires both --performance-weight and --price-weight")
	}
	if !isUnitWeight(rf.performanceWeight) {
		return nil, fmt.Errorf("--performance-weight must be a number between 0 and 1")
	}
	if !isUnitWeight(rf.priceWeight) {
		return nil, fmt.Errorf("--price-weight must be a number between 0 and 1")
	}
	if math.Abs(rf.performanceWeight+rf.priceWeight-1) > 1e-6 {
		return nil, fmt.Errorf("--performance-weight and --price-weight must sum to 1")
	}
	evalWeights, err := rf.evalWeights(cmd, currentConfig)
	if err != nil {
		return nil, err
	}
	if err := validateEvalWeights(evalWeights, evalIDs, rf.performanceWeight); err != nil {
		return nil, err
	}
	return &routerHeuristicConfig{
		PerformanceWeight: rf.performanceWeight,
		PriceWeight:       rf.priceWeight,
		EvalWeights:       evalWeights,
	}, nil
}

func (rf *routerConfigFlags) evalWeights(cmd *cobra.Command, currentConfig *routerHeuristicConfig) (map[string]float64, error) {
	if !cmd.Flags().Changed("eval-weight") {
		return currentEvalWeights(currentConfig), nil
	}
	evalWeights := map[string]float64{}
	for _, pair := range rf.evalWeightPairs {
		evalID, raw, err := splitPair(pair, "--eval-weight", "eval_id=WEIGHT")
		if err != nil {
			return nil, err
		}
		weight, err := strconv.ParseFloat(raw, 64)
		if err != nil || !isUnitWeight(weight) {
			return nil, fmt.Errorf("--eval-weight %s: weight must be a number between 0 and 1", pair)
		}
		evalWeights[evalID] = weight
	}
	return evalWeights, nil
}

func currentEvalWeights(config *routerHeuristicConfig) map[string]float64 {
	if config == nil {
		return map[string]float64{}
	}
	copy := map[string]float64{}
	for evalID, weight := range config.EvalWeights {
		copy[evalID] = weight
	}
	return copy
}

func validateEvalWeights(weights map[string]float64, evalIDs []string, performanceWeight float64) error {
	if len(weights) == 0 {
		if performanceWeight > 0 && len(evalIDs) == 0 {
			return fmt.Errorf("heuristic routing with --performance-weight > 0 requires at least one --eval")
		}
		if performanceWeight > 0 {
			return fmt.Errorf("heuristic routing with --performance-weight > 0 requires --eval-weight for every imported eval")
		}
		return nil
	}
	expected := map[string]bool{}
	for _, evalID := range evalIDs {
		expected[evalID] = true
	}
	if len(weights) != len(expected) {
		return fmt.Errorf("--eval-weight must be provided for exactly the imported eval ids")
	}
	total := 0.0
	for evalID, weight := range weights {
		if !expected[evalID] {
			return fmt.Errorf("--eval-weight %s: eval is not imported by this router", evalID)
		}
		if !isUnitWeight(weight) {
			return fmt.Errorf("--eval-weight %s: weight must be a number between 0 and 1", evalID)
		}
		total += weight
	}
	if math.Abs(total-1) > 1e-6 {
		return fmt.Errorf("--eval-weight values must sum to 1")
	}
	return nil
}

func newRouterCreateCmd(gf *globalFlags) *cobra.Command {
	rf := &routerConfigFlags{}
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a router for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			heuristic, err := rf.heuristicConfig(cmd, nil, rf.evalIDs)
			if err != nil {
				return err
			}
			strategy, err := routerStrategyForCreate(rf.strategy, heuristic != nil)
			if err != nil {
				return err
			}
			if strategy != "" {
				body.RoutingStrategy = strategy
			}
			body.HeuristicConfig = heuristic
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost, "/v1/organizations/current/routers", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	rf.register(cmd)
	return cmd
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
			currentEvalIDs := routerEvalIDs(current.Evals)
			targetEvalIDs := currentEvalIDs
			evalIDsChanged := rf.clearEvals || cmd.Flags().Changed("eval")
			switch {
			case rf.clearEvals:
				targetEvalIDs = []string{}
				body.EvalIDs = &targetEvalIDs
			case cmd.Flags().Changed("eval"):
				targetEvalIDs = rf.evalIDs
				body.EvalIDs = &targetEvalIDs
			}
			currentConfig := current.HeuristicConfig
			if evalIDsChanged && !cmd.Flags().Changed("eval-weight") {
				currentConfig = nil
			}
			heuristic, err := rf.heuristicConfig(cmd, currentConfig, targetEvalIDs)
			if err != nil {
				return err
			}
			strategy, err := routerStrategyForUpdate(rf.strategy, current.RoutingStrategy, heuristic != nil, evalIDsChanged)
			if err != nil {
				return err
			}
			if rf.strategy != "" || (heuristic != nil && strategy == "heuristic" && current.RoutingStrategy != "heuristic") {
				body.RoutingStrategy = strategy
			}
			body.HeuristicConfig = heuristic
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

func routerStrategyForCreate(strategy string, hasHeuristicConfig bool) (string, error) {
	switch {
	case strategy == "heuristic" && !hasHeuristicConfig:
		return "", fmt.Errorf("--strategy heuristic requires --performance-weight, --price-weight, and --eval-weight for every imported eval")
	case strategy == "slm" && hasHeuristicConfig:
		return "", fmt.Errorf("heuristic weight flags require --strategy heuristic")
	case strategy == "heuristic" || strategy == "slm":
		return strategy, nil
	case strategy != "":
		return "", fmt.Errorf("--strategy must be slm or heuristic")
	case hasHeuristicConfig:
		return "heuristic", nil
	default:
		return "", nil
	}
}

func routerStrategyForUpdate(strategy, currentStrategy string, hasHeuristicConfig, evalIDsChanged bool) (string, error) {
	if currentStrategy == "" {
		currentStrategy = "slm"
	}
	target := currentStrategy
	if strategy != "" {
		target = strategy
	} else if hasHeuristicConfig && currentStrategy != "heuristic" {
		target = "heuristic"
	}
	switch {
	case target == "heuristic" && !hasHeuristicConfig && (currentStrategy != "heuristic" || evalIDsChanged):
		return "", fmt.Errorf("heuristic routing requires --performance-weight, --price-weight, and --eval-weight for every imported eval")
	case target == "slm" && hasHeuristicConfig:
		return "", fmt.Errorf("heuristic weight flags require --strategy heuristic")
	case target != "heuristic" && target != "slm":
		return "", fmt.Errorf("--strategy must be slm or heuristic")
	default:
		return target, nil
	}
}

func routerEvalIDs(evals []routerEval) []string {
	ids := make([]string, 0, len(evals))
	for _, eval := range evals {
		ids = append(ids, eval.ID)
	}
	return ids
}

func isUnitWeight(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func splitPair(pair, flagName, format string) (string, string, error) {
	key, value, ok := strings.Cut(pair, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" || value == "" {
		return "", "", fmt.Errorf("invalid %s %q: expected %s", flagName, pair, format)
	}
	return key, value, nil
}
