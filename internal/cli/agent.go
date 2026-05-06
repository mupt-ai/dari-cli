package cli

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{Use: "agent", Short: "Manage deployed agents"}
		cmd.AddCommand(
			newAgentListCmd(gf),
			newAgentWebhookCmd(gf),
			newAgentDeleteCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newAgentListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List agents owned by the current organization.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp struct {
				Agents []any `json:"agents"`
			}
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents", nil, &resp); err != nil {
				return err
			}
			return printJSON(map[string]any{"agents": resp.Agents})
		},
	}
}

func newAgentWebhookCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "webhook", Short: "Manage agent webhooks"}
	cmd.AddCommand(
		newAgentWebhookGetCmd(gf),
		newAgentWebhookSetCmd(gf),
		newAgentWebhookClearCmd(gf),
		newAgentWebhookRotateCmd(gf),
	)
	return cmd
}

func newAgentWebhookGetCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <agent_id>",
		Short: "Show an agent webhook configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents/"+args[0]+"/webhook", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentWebhookSetCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "set <agent_id> <webhook_url>",
		Short: "Set an agent webhook URL for external tool requests.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"webhook_url": args[1]}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPut, "/v1/agents/"+args[0]+"/webhook", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentWebhookClearCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "clear <agent_id>",
		Short: "Clear an agent webhook configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodDelete, "/v1/agents/"+args[0]+"/webhook", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentWebhookRotateCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-secret <agent_id>",
		Short: "Rotate an agent webhook signing secret.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost, "/v1/agents/"+args[0]+"/webhook/rotate-secret", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentDeleteCmd(gf *globalFlags) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <agent_id>",
		Short: "Soft-delete an agent owned by the current organization.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]
			if !yes && !confirm(fmt.Sprintf("Delete agent %s? This is soft-delete; the agent becomes unpublished. [y/N] ", agentID)) {
				return fmt.Errorf("aborted")
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodDelete, "/v1/agents/"+agentID, nil, &resp); err != nil {
				return err
			}
			if resp == nil {
				resp = map[string]any{"agent_id": agentID, "deleted": true}
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the interactive confirmation prompt")
	return cmd
}

// confirm prints a yes/no prompt to stderr and reads a single line from stdin.
func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
