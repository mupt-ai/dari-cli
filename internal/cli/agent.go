package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/auth"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{Use: "agent", Short: "Manage deployed agents"}
		cmd.AddCommand(
			newAgentListCmd(gf),
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
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			var resp struct {
				Agents []any `json:"agents"`
			}
			if _, err := auth.DoAuthenticated(context.Background(), apiURL, http.MethodGet,
				"/v1/agents", nil, &resp); err != nil {
				return err
			}
			return printJSON(map[string]any{"agents": resp.Agents})
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
			if !yes {
				if !confirm(fmt.Sprintf("Delete agent %s? This is soft-delete; the agent becomes unpublished. [y/N] ", agentID)) {
					return fmt.Errorf("aborted")
				}
			}
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			var resp map[string]any
			if _, err := auth.DoAuthenticated(context.Background(), apiURL, http.MethodDelete,
				"/v1/agents/"+agentID, nil, &resp); err != nil {
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
