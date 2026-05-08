package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{Use: "session", Short: "Drive a conversation with a deployed agent"}
		cmd.AddCommand(
			newSessionCreateCmd(gf),
			newSessionGetCmd(gf),
			newSessionSendCmd(gf),
			newSessionEventsCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newSessionCreateCmd(gf *globalFlags) *cobra.Command {
	var agentRef string
	var sessionName string
	var secretAssignments []string
	var secretEnvNames []string
	var llmAPIKey string
	var llmAPIKeyEnv string
	var internetAccess bool
	var noInternetAccess bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a session for an agent.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if agentRef == "" {
				return errors.New("--agent is required")
			}
			agentID, err := resolveAgentRef(cmd, gf, agentRef)
			if err != nil {
				return err
			}
			secrets, err := resolveSessionSecrets(secretAssignments, secretEnvNames)
			if err != nil {
				return err
			}
			resolvedLLMAPIKey, err := resolveSessionLLMAPIKey(llmAPIKey, llmAPIKeyEnv)
			if err != nil {
				return err
			}
			internetAccessSet := cmd.Flags().Changed("internet-access")
			noInternetAccessSet := cmd.Flags().Changed("no-internet-access")
			if internetAccessSet && noInternetAccessSet {
				return errors.New("pass either --internet-access or --no-internet-access, not both")
			}
			body := map[string]any{}
			if cmd.Flags().Changed("name") {
				body["name"] = strings.TrimSpace(sessionName)
			}
			if len(secrets) > 0 {
				body["secrets"] = secrets
			}
			if resolvedLLMAPIKey != "" {
				body["llm_api_key"] = resolvedLLMAPIKey
			}
			if internetAccessSet {
				body["internet_access"] = internetAccess
			} else if noInternetAccessSet {
				body["internet_access"] = !noInternetAccess
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost,
				"/v1/agents/"+agentID+"/sessions", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&agentRef, "agent", "", "Agent ID or name to start a session against")
	cmd.Flags().StringVar(&sessionName, "name", "", "Optional user-visible session name")
	cmd.Flags().StringArrayVar(&secretAssignments, "secret", nil, "Runtime secret as NAME=VALUE (repeatable)")
	cmd.Flags().StringArrayVar(&secretEnvNames, "secret-env", nil, "Read runtime secret NAME from the local environment (repeatable)")
	cmd.Flags().StringVar(&llmAPIKey, "llm-api-key", "", "Override the session LLM provider API key")
	cmd.Flags().StringVar(&llmAPIKeyEnv, "llm-api-key-env", "", "Read the session LLM provider API key from this local environment variable")
	cmd.Flags().BoolVar(&internetAccess, "internet-access", false, "Allow public internet access from the execution sandbox")
	cmd.Flags().BoolVar(&noInternetAccess, "no-internet-access", false, "Disable public internet access from the execution sandbox")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func resolveSessionSecrets(assignments, envNames []string) (map[string]string, error) {
	secrets := map[string]string{}
	for _, assignment := range assignments {
		name, value, ok := strings.Cut(assignment, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --secret %q: expected NAME=VALUE", assignment)
		}
		if value == "" {
			return nil, fmt.Errorf("invalid --secret %q: value must be non-empty", name)
		}
		if _, exists := secrets[name]; exists {
			return nil, fmt.Errorf("duplicate runtime secret %q", name)
		}
		secrets[name] = value
	}
	for _, rawName := range envNames {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, errors.New("--secret-env requires a non-empty NAME")
		}
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			return nil, fmt.Errorf("environment variable %s is not set or is empty", name)
		}
		if _, exists := secrets[name]; exists {
			return nil, fmt.Errorf("duplicate runtime secret %q", name)
		}
		secrets[name] = value
	}
	return secrets, nil
}

func resolveSessionLLMAPIKey(inline, envName string) (string, error) {
	inline = strings.TrimSpace(inline)
	envName = strings.TrimSpace(envName)
	if inline != "" && envName != "" {
		return "", errors.New("pass either --llm-api-key or --llm-api-key-env, not both")
	}
	if inline != "" {
		return inline, nil
	}
	if envName == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(envName)
	if !ok || value == "" {
		return "", fmt.Errorf("environment variable %s is not set or is empty", envName)
	}
	return value, nil
}

func newSessionGetCmd(gf *globalFlags) *cobra.Command {
	var agentRef string
	cmd := &cobra.Command{
		Use:   "get <session>",
		Short: "Fetch the current state of a session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := resolveSessionRef(cmd, gf, args[0], agentRef)
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet,
				"/v1/sessions/"+sessionID, nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&agentRef, "agent", "", "Agent ID or name used to resolve a session name")
	return cmd
}

func newSessionSendCmd(gf *globalFlags) *cobra.Command {
	var useStdin bool
	var agentRef string
	cmd := &cobra.Command{
		Use:   "send <session> [text]",
		Short: "Send a user message to a session.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := resolveSessionRef(cmd, gf, args[0], agentRef)
			if err != nil {
				return err
			}
			text, err := resolveSessionText(args, useStdin)
			if err != nil {
				return err
			}
			body := map[string]any{
				"type": "user.message",
				"content": []map[string]any{{
					"type": "text",
					"text": text,
				}},
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost,
				"/v1/sessions/"+sessionID+"/events", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&useStdin, "stdin", false, "Read the message body from standard input")
	cmd.Flags().StringVar(&agentRef, "agent", "", "Agent ID or name used to resolve a session name")
	return cmd
}

func newSessionEventsCmd(gf *globalFlags) *cobra.Command {
	var limit int
	var agentRef string
	cmd := &cobra.Command{
		Use:   "events <session>",
		Short: "List persisted events for a session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := resolveSessionRef(cmd, gf, args[0], agentRef)
			if err != nil {
				return err
			}
			path := "/v1/sessions/" + sessionID + "/events"
			if limit > 0 {
				q := url.Values{}
				q.Set("limit", fmt.Sprintf("%d", limit))
				path += "?" + q.Encode()
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, path, nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of events to return (0 = server default)")
	cmd.Flags().StringVar(&agentRef, "agent", "", "Agent ID or name used to resolve a session name")
	return cmd
}

// resolveSessionText picks the message body from a positional arg or stdin,
// rejecting ambiguity and empty input.
func resolveSessionText(args []string, useStdin bool) (string, error) {
	explicit := len(args) == 2
	if explicit && useStdin {
		return "", errors.New("pass either TEXT or --stdin, not both")
	}
	if useStdin {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		text := strings.TrimRight(string(raw), "\r\n")
		if text == "" {
			return "", errors.New("message body must be non-empty")
		}
		return text, nil
	}
	if !explicit {
		return "", errors.New("message text is required (pass as second argument or use --stdin)")
	}
	if args[1] == "" {
		return "", errors.New("message body must be non-empty")
	}
	return args[1], nil
}
