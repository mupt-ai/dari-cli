package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/deploy"
	"github.com/mupt-ai/dari-cli/internal/state"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		root.AddCommand(newDeployCmd(gf))
	})
}

func newDeployCmd(gf *globalFlags) *cobra.Command {
	var (
		apiKey  string
		agentID string
		dryRun  bool
		quiet   bool
	)
	cmd := &cobra.Command{
		Use:   "deploy [repo_root]",
		Short: "Package the current checkout and publish it to Dari.",
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

			if dryRun {
				prepared, err := deploy.Prepare(repoRoot, apiURL, agentID)
				if err != nil {
					return err
				}
				return printJSON(prepared.DryRunPayload())
			}

			resolvedKey := apiKey
			if resolvedKey == "" {
				resolvedKey = auth.EnvAPIKeyValue()
			}
			if resolvedKey == "" {
				// Fall back to the cached managed CLI key for the current org.
				s, err := state.Load()
				if err != nil {
					return err
				}
				if api.URLsMatch(s.APIURL, apiURL) {
					if org := s.CurrentOrg(); org != nil {
						resolvedKey = org.APIKey
					}
				}
			}
			if resolvedKey == "" {
				return errors.New("DARI_API_KEY is required unless --dry-run is set or CLI login has selected an organization")
			}

			resolvedAgentID := strings.TrimSpace(agentID)
			if resolvedAgentID != "" && !strings.HasPrefix(resolvedAgentID, "agt_") {
				var agentsResp struct {
					Agents []agentReferenceSummary `json:"agents"`
				}
				client := api.New(apiURL).WithBearer(resolvedKey)
				if err := client.Do(cmd.Context(), http.MethodGet, "/v1/agents", nil, &agentsResp); err != nil {
					return err
				}
				resolvedAgentID, err = matchAgentRef(resolvedAgentID, agentsResp.Agents)
				if err != nil {
					return err
				}
			}
			cfg := deploy.Config{
				APIURL:  apiURL,
				APIKey:  resolvedKey,
				AgentID: resolvedAgentID,
			}
			if !quiet {
				cfg.Progress = deploy.NewConsoleProgress(os.Stderr).Handle
			}
			response, err := deploy.Execute(cmd.Context(), repoRoot, cfg)
			if err != nil {
				return translateDeployError(err)
			}
			return printJSON(response)
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Bearer token for the Dari API (falls back to $DARI_API_KEY or the cached CLI login)")
	cmd.Flags().StringVar(&agentID, "agent-id", os.Getenv("DARI_AGENT_ID"), "Existing agent ID or name to publish a new version for")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the prepared publish request instead of sending it")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress per-stage deploy progress on stderr")
	return cmd
}

// translateDeployError strips HTTPError wrapping so user-facing errors are
// cleaner. Login/permission failures get a hint about `dari auth login`.
func translateDeployError(err error) error {
	if he := api.AsHTTPError(err); he != nil && (he.Status == 401 || he.Status == 403) {
		return fmt.Errorf("%s (run `dari auth login` or pass --api-key)", strings.TrimSpace(he.Detail))
	}
	return api.HumanError(err)
}
