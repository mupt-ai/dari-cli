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

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/cli/agents"
	"github.com/mupt-ai/dari-cli/internal/cli/agents/claude"
	"github.com/mupt-ai/dari-cli/internal/cli/agents/codex"
	"github.com/mupt-ai/dari-cli/internal/cli/agents/opencode"
	"github.com/mupt-ai/dari-cli/internal/cli/agents/pi"
	"github.com/mupt-ai/dari-cli/internal/state"
)

const (
	routingBaseURL = "https://routing.dari.dev"
)

// agentLaunchers maps each agent subcommand to its help text. Execute
// dispatches them before Cobra parses anything, so every following argument
// reaches the agent untouched. Cobra only sees them when a Dari flag comes
// first, which would be dropped, so those commands refuse to run.
var agentLaunchers = map[string]string{
	"claude":   "Launch Claude Code through Dari; forward all following arguments (--connect manages subscriptions)",
	"codex":    "Launch Codex through Dari; forward all following arguments (--connect manages subscriptions)",
	"opencode": "Launch OpenCode through Dari; forward all following arguments (--connect manages subscriptions)",
	"pi":       "Launch Pi through Dari; forward all following arguments (--connect manages subscriptions)",
}

// agentConnectFlag opens the subscription list before launching. Dari removes
// it, so the agent never sees it.
const agentConnectFlag = "--connect"

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, _ *globalFlags) {
		for name, short := range agentLaunchers {
			root.AddCommand(&cobra.Command{
				Use:                name + " [args...]",
				Short:              short,
				DisableFlagParsing: true,
				RunE: func(*cobra.Command, []string) error {
					return fmt.Errorf("%q must be the first argument so Dari can forward all agent arguments", name)
				},
			})
		}
	})
}

type agentLaunch struct {
	name    string
	args    []string
	connect bool
}

func parseAgentLaunch(args []string) (agentLaunch, bool) {
	if len(args) == 0 {
		return agentLaunch{}, false
	}

	if _, ok := agentLaunchers[args[0]]; !ok {
		return agentLaunch{}, false
	}
	launch := agentLaunch{name: args[0], args: []string{}}
	for i, arg := range args[1:] {
		if arg == "--" {
			launch.args = append(launch.args, args[1+i:]...)
			break
		}
		if arg == agentConnectFlag {
			launch.connect = true
			continue
		}
		launch.args = append(launch.args, arg)
	}
	return launch, true
}

func runAgentLaunch(ctx context.Context, launch agentLaunch, stdin io.Reader, stdout, stderr io.Writer) int {
	path, err := agentExecutable(launch.name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 127
	}

	key := "unused"
	routerID := "default"
	var models agents.Models
	if !agents.OnlyRequestsInfo(launch.args) {
		var access agentRoutingAccess
		access, err = resolveAgentRoutingAccess(ctx, stdin, stderr)
		if err == nil && launch.connect {
			err = connectAgentSubscriptionsOnRequest(ctx, launch.name, access, stdin, stderr)
		}
		if err == nil {
			key = access.key
			routerID, err = ensureAgentRouter(ctx, launch.name, access, stdin, stderr)
		}
		if err == nil {
			models, err = agentModels(ctx, launch.name, access, routerID, stderr)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	command, err := buildAgentCommand(path, launch, key, routerID, models)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// Read cleanup at return time: catalog and display setup compose resources.
	defer func() {
		if command.Cleanup != nil {
			command.Cleanup()
		}
	}()
	if launch.name == "codex" && !agents.OnlyRequestsInfo(launch.args) {
		if err := codex.PrepareCatalog(ctx, &command, key, agentRoutingBaseURL(routerID), models); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := codex.PrepareStatus(ctx, &command, launch.args, key, agentRoutingBaseURL(routerID)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Env = command.Env
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

func resolveAgentRoutingKey(ctx context.Context, stdin io.Reader, stderr io.Writer) (string, error) {
	if key := strings.TrimSpace(os.Getenv(agents.RoutingAPIKeyEnv)); key != "" {
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
	if key := strings.TrimSpace(os.Getenv(agents.RoutingAPIKeyEnv)); key != "" {
		return key, nil
	}
	key, created, err := state.EnsureRoutingKey(scope, func() (string, error) {
		return createAgentRoutingKey(ctx, apiURL)
	})
	if err != nil {
		return "", err
	}
	if created {
		agentStep(stderr, "Routing key created")
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

func buildAgentCommand(path string, launch agentLaunch, key, routerID string, models agents.Models) (agents.Command, error) {
	command := agents.Command{Path: path, Args: launch.args, Env: agents.WithEnvironment(os.Environ(), map[string]string{agents.RoutingAPIKeyEnv: key})}
	baseURL := agentRoutingBaseURL(routerID)
	switch launch.name {
	case "claude":
		if err := claude.Configure(&command, key, baseURL, models); err != nil {
			return agents.Command{}, err
		}
	case "codex":
		codex.Configure(&command, baseURL, models)
	case "opencode":
		if err := opencode.Configure(&command, baseURL, models); err != nil {
			return agents.Command{}, err
		}
	case "pi":
		if err := pi.Configure(&command, models); err != nil {
			return agents.Command{}, err
		}
	default:
		return agents.Command{}, fmt.Errorf("unsupported coding agent %q", launch.name)
	}
	return command, nil
}

func agentRoutingBaseURL(routerID string) string {
	baseURL := routingBaseURL
	if routerID != "" && routerID != "default" {
		baseURL += "/" + routerID
	}
	return baseURL
}
