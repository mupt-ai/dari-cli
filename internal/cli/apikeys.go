package cli

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
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
			var resp struct {
				APIKeys []any `json:"api_keys"`
			}
			if err := orgJWTRequest(cmd, gf, http.MethodGet, "/api-keys", nil, &resp); err != nil {
				return err
			}
			return printJSON(map[string]any{"api_keys": resp.APIKeys})
		},
	}
}

func newAPIKeysCreateCmd(gf *globalFlags) *cobra.Command {
	var (
		name   string
		scopes []string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new manual API key for the current org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			normalizedScopes, err := normalizeAPIKeyScopes(scopes)
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgJWTRequest(cmd, gf, http.MethodPost, "/api-keys",
				map[string]any{"label": name, "scopes": normalizedScopes}, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Key label")
	cmd.Flags().StringSliceVar(&scopes, "scope", []string{"platform"}, "Key scope (platform for CLI/API use, routing for router traffic); may be repeated or comma-separated")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func normalizeAPIKeyScopes(scopes []string) ([]string, error) {
	allowed := map[string]bool{"platform": true, "routing": true}
	normalized := make([]string, 0, len(scopes))
	seen := map[string]bool{}
	for _, raw := range scopes {
		for _, part := range strings.Split(raw, ",") {
			scope := strings.ToLower(strings.TrimSpace(part))
			if scope == "" {
				continue
			}
			if !allowed[scope] {
				return nil, fmt.Errorf("unsupported API key scope %q (supported: platform, routing)", scope)
			}
			if seen[scope] {
				continue
			}
			seen[scope] = true
			normalized = append(normalized, scope)
		}
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one API key scope is required (supported: platform, routing)")
	}
	return normalized, nil
}

func newAPIKeysRevokeCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <key_id>",
		Short: "Revoke an API key for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgJWTRequest(cmd, gf, http.MethodDelete, "/api-keys/"+args[0], nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}
