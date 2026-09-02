package cli

import (
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{
			Use:     "eval",
			Aliases: []string{"evals"},
			Short:   "Inspect eval scorecards for the current org",
		}
		cmd.AddCommand(
			newEvalCreateCmd(gf),
			newEvalListCmd(gf),
			newEvalGetCmd(gf),
			newEvalOfficialCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newEvalListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List eval scorecards visible to the current org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/organizations/current/evals", nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func newEvalOfficialCmd(gf *globalFlags) *cobra.Command {
	var modelIDs []string
	var benchmarks []string
	var publishers []string
	cmd := &cobra.Command{
		Use:   "official",
		Short: "List Dari eval results from official model-company reports",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := url.Values{}
			for _, value := range modelIDs {
				params.Add("model_id", value)
			}
			for _, value := range benchmarks {
				params.Add("benchmark", value)
			}
			for _, value := range publishers {
				params.Add("publisher", value)
			}
			path := "/v1/organizations/current/dari-evals"
			if query := params.Encode(); query != "" {
				path += "?" + query
			}
			var response map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, path, nil, &response); err != nil {
				return err
			}
			return printJSON(response)
		},
	}
	cmd.Flags().StringSliceVar(&modelIDs, "model", nil, "Filter by exact Dari model slug (repeatable)")
	cmd.Flags().StringSliceVar(&benchmarks, "benchmark", nil, "Filter by exact benchmark name (repeatable)")
	cmd.Flags().StringSliceVar(&publishers, "publisher", nil, "Filter by exact publisher name (repeatable)")
	return cmd
}

func newEvalGetCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <eval_id>",
		Short: "Show one eval scorecard for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/organizations/current/evals/"+url.PathEscape(args[0]), nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}
