package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/state"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		org := &cobra.Command{Use: "org", Short: "Manage organizations"}
		org.AddCommand(
			newOrgListCmd(gf),
			newOrgCreateCmd(gf),
			newOrgSwitchCmd(gf),
			newOrgMembersCmd(gf),
			newOrgInviteCmd(gf),
		)
		root.AddCommand(org)
	})
}

func newOrgListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available orgs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			s, orgs, err := auth.ListOrganizations(context.Background(), apiURL)
			if err != nil {
				return err
			}
			return printJSON(map[string]any{
				"current_org_id": nilIfEmpty(s.CurrentOrgID),
				"organizations":  orgs,
			})
		},
	}
}

func newOrgCreateCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			s, err := auth.CreateOrganization(context.Background(), apiURL, args[0])
			if err != nil {
				return err
			}
			return printJSON(orgCreateOrSwitchOutput(s))
		},
	}
}

func newOrgSwitchCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <organization>",
		Short: "Switch the current org by slug or ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			s, err := auth.SwitchOrganization(context.Background(), apiURL, args[0])
			if err != nil {
				return err
			}
			return printJSON(orgCreateOrSwitchOutput(s))
		},
	}
}

func newOrgMembersCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "members",
		Short: "List members in the current org",
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
				Members []any `json:"members"`
			}
			if _, err := auth.DoAuthenticated(context.Background(), apiURL, http.MethodGet,
				"/v1/organizations/"+orgID+"/members", nil, &resp); err != nil {
				return err
			}
			return printJSON(map[string]any{"members": resp.Members})
		},
	}
}

func newOrgInviteCmd(gf *globalFlags) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "invite <email>",
		Short: "Invite a user to the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if role != "owner" && role != "admin" && role != "member" {
				return fmt.Errorf("invalid role %q: expected owner, admin, or member", role)
			}
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
				"/v1/organizations/"+orgID+"/invitations",
				map[string]string{"email": args[0], "role": role}, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().StringVar(&role, "role", "member", "Membership role for the invite (owner|admin|member)")
	return cmd
}

func orgCreateOrSwitchOutput(s *state.CliState) map[string]any {
	out := map[string]any{
		"current_org_id": nilIfEmpty(s.CurrentOrgID),
		"organization":   nil,
	}
	if org := s.CurrentOrg(); org != nil {
		out["organization"] = orgToMap(*org)
	}
	return out
}

// requireCurrentOrgID loads state and asserts a current org is selected.
// Does no network calls — bails early if the user needs to run `dari org switch`.
func requireCurrentOrgID() (string, error) {
	s, err := state.Load()
	if err != nil {
		return "", err
	}
	if s.CurrentOrgID == "" {
		return "", auth.ErrNoCurrentOrg
	}
	return s.CurrentOrgID, nil
}

