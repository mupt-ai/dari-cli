package cli

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/deploy"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{Use: "agent", Short: "Manage deployed agents"}
		cmd.AddCommand(
			newAgentListCmd(gf),
			newAgentVersionsCmd(gf),
			newAgentVersionCmd(gf),
			newAgentStatusCmd(gf),
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

func newAgentVersionsCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "versions <agent>",
		Short: "List published versions for an agent.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents/"+agentID+"/versions", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentVersionCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "version", Short: "Inspect one published agent version"}
	cmd.AddCommand(
		newAgentVersionShowCmd(gf),
		newAgentVersionFilesCmd(gf),
		newAgentVersionCatCmd(gf),
	)
	return cmd
}

func newAgentVersionShowCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "show <agent> <version_id>",
		Short: "Show published version metadata.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents/"+agentID+"/versions/"+args[1], nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentVersionFilesCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "files <agent> <version_id>",
		Short: "List files in a version source bundle.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents/"+agentID+"/versions/"+args[1]+"/bundle", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentVersionCatCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "cat <agent> <version_id> <path>",
		Short: "Print one UTF-8 file from a version source bundle.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			var resp struct {
				Content string `json:"content"`
			}
			path := "/v1/agents/" + agentID + "/versions/" + args[1] + "/bundle/file?path=" + url.QueryEscape(args[2])
			if err := orgKeyRequest(cmd, gf, http.MethodGet, path, nil, &resp); err != nil {
				return err
			}
			fmt.Print(resp.Content)
			return nil
		},
	}
}

func newAgentStatusCmd(gf *globalFlags) *cobra.Command {
	var agentID string
	cmd := &cobra.Command{
		Use:   "status [repo_root]",
		Short: "Check whether the local bundle matches the active published version.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot := "."
			if len(args) == 1 {
				repoRoot = args[0]
			}
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			resolvedAgentID := strings.TrimSpace(agentID)
			if resolvedAgentID == "" {
				resolvedAgentID = strings.TrimSpace(os.Getenv("DARI_AGENT_ID"))
			}
			prepared, err := deploy.Prepare(repoRoot, apiURL, resolvedAgentID)
			if err != nil {
				return err
			}
			if prepared.AgentID == "" {
				return fmt.Errorf("no agent id found; pass --agent-id, set DARI_AGENT_ID, or run dari deploy once from this repo")
			}
			resolvedAgentID, err = resolveAgentRef(cmd, gf, prepared.AgentID)
			if err != nil {
				return err
			}

			var resp struct {
				Agent struct {
					ID              string `json:"id"`
					ActiveVersionID string `json:"active_version_id"`
				} `json:"agent"`
				Versions []struct {
					ID            string `json:"id"`
					IsActive      bool   `json:"is_active"`
					ArchiveSHA256 string `json:"archive_sha256"`
					SizeBytes     *int64 `json:"size_bytes"`
				} `json:"versions"`
			}
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents/"+resolvedAgentID+"/versions", nil, &resp); err != nil {
				return err
			}
			activeVersionID := resp.Agent.ActiveVersionID
			activeSHA := ""
			for _, version := range resp.Versions {
				if version.IsActive || version.ID == activeVersionID {
					activeVersionID = version.ID
					activeSHA = version.ArchiveSHA256
					break
				}
			}
			return printJSON(map[string]any{
				"agent_id":          resolvedAgentID,
				"active_version_id": activeVersionID,
				"local_sha256":      prepared.Bundle.SHA256,
				"active_sha256":     activeSHA,
				"up_to_date":        activeSHA != "" && activeSHA == prepared.Bundle.SHA256,
			})
		},
	}
	cmd.Flags().StringVar(&agentID, "agent-id", "", "Existing agent ID to compare against (falls back to $DARI_AGENT_ID or .dari/deploy-state.json)")
	return cmd
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
		Use:   "get <agent>",
		Short: "Show an agent webhook configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/agents/"+agentID+"/webhook", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentWebhookSetCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "set <agent> <webhook_url>",
		Short: "Set an agent webhook URL for external tool requests.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			body := map[string]any{"webhook_url": args[1]}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPut, "/v1/agents/"+agentID+"/webhook", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentWebhookClearCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "clear <agent>",
		Short: "Clear an agent webhook configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodDelete, "/v1/agents/"+agentID+"/webhook", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentWebhookRotateCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-secret <agent>",
		Short: "Rotate an agent webhook signing secret.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost, "/v1/agents/"+agentID+"/webhook/rotate-secret", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newAgentDeleteCmd(gf *globalFlags) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <agent>",
		Short: "Soft-delete an agent owned by the current organization.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, err := resolveAgentRef(cmd, gf, args[0])
			if err != nil {
				return err
			}
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
