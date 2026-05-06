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
	var agentID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a session for an agent.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if agentID == "" {
				return errors.New("--agent is required")
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost,
				"/v1/agents/"+agentID+"/sessions", map[string]any{}, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "Agent ID to start a session against")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func newSessionGetCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <session_id>",
		Short: "Fetch the current state of a session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet,
				"/v1/sessions/"+args[0], nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newSessionSendCmd(gf *globalFlags) *cobra.Command {
	var useStdin bool
	cmd := &cobra.Command{
		Use:   "send <session_id> [text]",
		Short: "Send a user message to a session.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
				"/v1/sessions/"+args[0]+"/events", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&useStdin, "stdin", false, "Read the message body from standard input")
	return cmd
}

func newSessionEventsCmd(gf *globalFlags) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "events <session_id>",
		Short: "List persisted events for a session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/v1/sessions/" + args[0] + "/events"
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
