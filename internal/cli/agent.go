package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/state"
)

const (
	routingAPIKeyEnv = "DARI_ROUTING_API_KEY"
	routingBaseURL   = "https://routing.dari.dev"
	routingModel     = "dari/routing"
)

type agentLaunch struct {
	name string
	args []string
}

type agentCommand struct {
	path    string
	args    []string
	env     []string
	cleanup func()
}

func parseAgentLaunch(args []string) (agentLaunch, bool) {
	if len(args) == 0 {
		return agentLaunch{}, false
	}

	var name string
	switch args[0] {
	case "--claude":
		name = "claude"
	case "--codex":
		name = "codex"
	case "--pi":
		name = "pi"
	default:
		return agentLaunch{}, false
	}
	return agentLaunch{name: name, args: append([]string(nil), args[1:]...)}, true
}

func runAgentLaunch(ctx context.Context, launch agentLaunch, stdin io.Reader, stdout, stderr io.Writer) int {
	path, err := agentExecutable(launch.name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 127
	}

	key := "unused"
	routerID := "default"
	if !onlyRequestsAgentInfo(launch.args) {
		if launch.name == "pi" {
			key, err = resolveAgentRoutingKey(ctx, stdin, stderr)
		} else {
			var access agentRoutingAccess
			access, err = resolveAgentRoutingAccess(ctx, stdin, stderr)
			if err == nil {
				key = access.key
				routerID, err = ensureAgentRouter(ctx, launch.name, access, stdin, stderr)
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	command, err := buildAgentCommand(path, launch, key, routerID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if command.cleanup != nil {
		defer command.cleanup()
	}
	cmd := exec.CommandContext(ctx, command.path, command.args...)
	cmd.Env = command.env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "start %s: %v\n", launch.name, err)
		return 1
	}
	return 0
}

func agentExecutable(name string) (string, error) {
	envName := "DARI_" + strings.ToUpper(name) + "_PATH"
	if path := strings.TrimSpace(os.Getenv(envName)); path != "" {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("%s does not point to an executable file", envName)
		}
		return resolved, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s is not installed or is not on PATH", name)
	}
	return path, nil
}

func onlyRequestsAgentInfo(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch arg {
		case "--help", "-h", "--version", "-V", "-v":
			return true
		}
	}
	return len(args) > 0 && args[0] == "help"
}

func resolveAgentRoutingKey(ctx context.Context, stdin io.Reader, stderr io.Writer) (string, error) {
	if key := strings.TrimSpace(os.Getenv(routingAPIKeyEnv)); key != "" {
		return key, nil
	}

	apiURL, err := (&globalFlags{}).resolveAPIURL()
	if err != nil {
		return "", err
	}
	scope, err := agentKeyScope(ctx, apiURL, stdin, stderr)
	if err != nil {
		return "", err
	}
	return resolveAgentRoutingKeyForScope(ctx, apiURL, scope, stderr)
}

func resolveAgentRoutingKeyForScope(
	ctx context.Context,
	apiURL string,
	scope string,
	stderr io.Writer,
) (string, error) {
	if key := strings.TrimSpace(os.Getenv(routingAPIKeyEnv)); key != "" {
		return key, nil
	}
	key, created, err := state.EnsureRoutingKey(scope, func() (string, error) {
		return createAgentRoutingKey(ctx, apiURL)
	})
	if err != nil {
		return "", err
	}
	if created {
		fmt.Fprintln(stderr, "Created and saved a Routing key for Dari coding agents.")
	}
	return key, nil
}

func agentKeyScope(ctx context.Context, apiURL string, stdin io.Reader, stderr io.Writer) (string, error) {
	if managementKey := auth.EnvAPIKeyValue(); managementKey != "" {
		digest := sha256.Sum256([]byte(managementKey))
		return apiURL + "|key:" + hex.EncodeToString(digest[:8]), nil
	}

	s, err := state.Load()
	if err != nil {
		return "", err
	}
	if !api.URLsMatch(s.APIURL, apiURL) || s.CurrentOrg() == nil {
		fmt.Fprintln(stderr, "Sign in to create a Routing key for your coding agents.")
		s, err = auth.LoginWithOptions(ctx, apiURL, auth.LoginOptions{Stdin: stdin})
		if err != nil {
			return "", err
		}
	}
	org := s.CurrentOrg()
	if org == nil {
		return "", auth.ErrNoCurrentOrg
	}
	if org.APIKey == "" {
		s, _, err = auth.ListOrganizations(ctx, apiURL)
		if err != nil {
			return "", err
		}
		org = s.CurrentOrg()
		if org == nil {
			return "", auth.ErrNoCurrentOrg
		}
	}
	return apiURL + "|org:" + org.ID, nil
}

func createAgentRoutingKey(ctx context.Context, apiURL string) (string, error) {
	key, err := issueAgentRoutingKey(ctx, apiURL)
	httpErr := api.AsHTTPError(err)
	if httpErr != nil && httpErr.Status == http.StatusUnauthorized && auth.EnvAPIKeyValue() == "" {
		s, loadErr := state.Load()
		if loadErr == nil && s.CurrentOrg() != nil {
			if _, refreshErr := auth.SwitchOrganization(ctx, apiURL, s.CurrentOrgID); refreshErr == nil {
				key, err = issueAgentRoutingKey(ctx, apiURL)
			}
		}
	}
	if err != nil {
		return "", api.HumanError(err)
	}
	if strings.TrimSpace(key) == "" {
		return "", errors.New("Dari returned an empty Routing key")
	}
	return key, nil
}

func issueAgentRoutingKey(ctx context.Context, apiURL string) (string, error) {
	client, err := auth.OrgKeyClient(apiURL)
	if err != nil {
		return "", err
	}
	hostname, _ := os.Hostname()
	label := "Dari coding agents"
	if hostname != "" {
		label += " (" + hostname + ")"
	}
	var issued struct {
		APIKey string `json:"api_key"`
	}
	err = client.Do(
		ctx,
		http.MethodPost,
		"/v1/organizations/current/api-keys",
		map[string]string{"label": label, "key_type": "routing"},
		&issued,
	)
	return issued.APIKey, err
}

func buildAgentCommand(path string, launch agentLaunch, key, routerID string) (agentCommand, error) {
	env := withEnvironment(os.Environ(), map[string]string{routingAPIKeyEnv: key})
	var (
		ownedArgs []string
		cleanup   func()
	)

	switch launch.name {
	case "claude":
		baseURL := routingBaseURL
		if routerID != "" && routerID != "default" {
			baseURL += "/" + routerID
		}
		env = withEnvironment(env, map[string]string{
			"ANTHROPIC_BASE_URL":             baseURL,
			"ANTHROPIC_AUTH_TOKEN":           key,
			"ANTHROPIC_API_KEY":              "",
			"CLAUDE_CODE_EFFORT_LEVEL":       "auto",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000",
			"CLAUDE_CODE_SUBAGENT_MODEL":     routingModel,
		})
		ownedArgs = []string{"--model", routingModel}
	case "codex":
		ownedArgs = []string{
			"--config", `model="dari/routing"`,
			"--config", `model_provider="dari"`,
			"--config", `model_reasoning_summary="none"`,
			"--config", `model_providers.dari.name="Dari"`,
			"--config", `model_providers.dari.base_url="https://routing.dari.dev/v1"`,
			"--config", `model_providers.dari.env_key="DARI_ROUTING_API_KEY"`,
			"--config", `model_providers.dari.wire_api="responses"`,
		}
	case "pi":
		extensionPath, err := writePiProviderExtension()
		if err != nil {
			return agentCommand{}, err
		}
		cleanup = func() { _ = os.Remove(extensionPath) }
		ownedArgs = []string{
			"--extension", extensionPath,
			"--provider", "dari",
			"--model", routingModel,
		}
	default:
		return agentCommand{}, fmt.Errorf("unsupported coding agent %q", launch.name)
	}

	return agentCommand{
		path:    path,
		args:    insertBeforeDoubleDash(launch.args, ownedArgs),
		env:     env,
		cleanup: cleanup,
	}, nil
}

func insertBeforeDoubleDash(args, inserted []string) []string {
	at := len(args)
	for i, arg := range args {
		if arg == "--" {
			at = i
			break
		}
	}
	result := make([]string, 0, len(args)+len(inserted))
	result = append(result, args[:at]...)
	result = append(result, inserted...)
	result = append(result, args[at:]...)
	return result
}

func withEnvironment(base []string, values map[string]string) []string {
	result := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[name]; !replaced {
			result = append(result, entry)
		}
	}
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

const piProviderExtension = `export default function (pi: any) {
  pi.registerProvider("dari", {
    name: "Dari",
    baseUrl: "https://routing.dari.dev/v1",
    apiKey: "$DARI_ROUTING_API_KEY",
    api: "openai-completions",
    compat: { sendSessionAffinityHeaders: true },
    models: [{
      id: "dari/routing",
      name: "Dari Router",
      reasoning: false,
      input: ["text", "image"],
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      contextWindow: 1000000,
      maxTokens: 128000
    }]
  });
}
`

func writePiProviderExtension() (string, error) {
	file, err := os.CreateTemp("", "dari-pi-provider-*.ts")
	if err != nil {
		return "", fmt.Errorf("create temporary Pi provider: %w", err)
	}
	path := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("set Pi provider permissions: %w", err)
	}
	if _, err = file.WriteString(piProviderExtension); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write Pi provider: %w", err)
	}
	if err = file.Close(); err != nil {
		return "", fmt.Errorf("close Pi provider: %w", err)
	}
	return path, nil
}
