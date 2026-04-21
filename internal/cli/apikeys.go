package cli

import (
	"context"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/auth"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{Use: "api-keys", Short: "Manage API keys for the current org"}
		cmd.AddCommand(
			newAPIKeysListCmd(gf),
			newAPIKeysCreateCmd(gf),
			newAPIKeysRevokeCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newAPIKeysListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List API keys for the current org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			orgID, err := requireCurrentOrgID()
			if err != nil {
				return err
			}
			var resp struct {
				APIKeys []any `json:"api_keys"`
			}
			if _, err := auth.DoAuthenticated(context.Background(), apiURL, http.MethodGet,
				"/v1/organizations/"+orgID+"/api-keys", nil, &resp); err != nil {
				return err
			}
			return printJSON(map[string]any{"api_keys": resp.APIKeys})
		},
	}
}

func newAPIKeysCreateCmd(gf *globalFlags) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new manual API key for the current org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			orgID, err := requireCurrentOrgID()
			if err != nil {
				return err
			}
			var resp map[string]any
			if _, err := auth.DoAuthenticated(context.Background(), apiURL, http.MethodPost,
				"/v1/organizations/"+orgID+"/api-keys",
				map[string]string{"label": name}, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Key label")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newAPIKeysRevokeCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <key_id>",
		Short: "Revoke an API key for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			orgID, err := requireCurrentOrgID()
			if err != nil {
				return err
			}
			var resp map[string]any
			if _, err := auth.DoAuthenticated(context.Background(), apiURL, http.MethodDelete,
				"/v1/organizations/"+orgID+"/api-keys/"+args[0], nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}
