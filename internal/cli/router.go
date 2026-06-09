package cli

import (
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/deploy"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{
			Use:     "router",
			Aliases: []string{"routers"},
			Short:   "Inspect Dari Routers for the current org",
		}
		cmd.AddCommand(
			newRouterListCmd(gf),
			newRouterGetCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newRouterListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List routers for the current org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp map[string]any
			if err := orgJWTRequest(cmd, gf, http.MethodGet, "/routers", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newRouterGetCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <router_id_or_endpoint>",
		Short: "Show one router for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			routerID, err := deploy.NormalizeRouterID(args[0])
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgJWTRequest(cmd, gf, http.MethodGet, "/routers/"+url.PathEscape(routerID), nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}
