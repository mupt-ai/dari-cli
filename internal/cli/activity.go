package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/auth"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		activity := &cobra.Command{
			Use:   "activity",
			Short: "Read routing activity for the current org",
		}
		activity.AddCommand(newActivityModelsCmd(gf))
		root.AddCommand(activity)
	})
}

type activityModelsFlags struct {
	from           string
	to             string
	routerID       string
	apiKeyIDs      []string
	userIDs        []string
	models         []string
	provider       string
	status         string
	organizationID string
}

func newActivityModelsCmd(gf *globalFlags) *cobra.Command {
	flags := &activityModelsFlags{}
	cmd := &cobra.Command{
		Use:     "models",
		Aliases: []string{"model-usage"},
		Short:   "Show model usage, cost, latency, outcomes, and route switches",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			query, err := flags.query()
			if err != nil {
				return err
			}
			var response map[string]any
			organizationID := strings.TrimSpace(flags.organizationID)
			path := activityModelsPath("current", query)
			if organizationID == "" {
				if err := orgKeyRequest(
					cmd,
					gf,
					http.MethodGet,
					path,
					nil,
					&response,
				); err != nil {
					return err
				}
				return printJSON(response)
			}
			if auth.EnvAPIKeyValue() != "" {
				return fmt.Errorf(
					"--organization-id requires browser login; unset DARI_API_KEY and run `dari auth login`",
				)
			}
			apiURL, err := gf.resolveAPIURL()
			if err != nil {
				return err
			}
			path = activityModelsPath(organizationID, query)
			if _, err := auth.DoAuthenticated(
				cmd.Context(),
				apiURL,
				http.MethodGet,
				path,
				nil,
				&response,
			); err != nil {
				return err
			}
			return printJSON(response)
		},
	}

	commandFlags := cmd.Flags()
	commandFlags.StringVar(
		&flags.from,
		"from",
		"",
		"Range start in RFC3339 format (inclusive)",
	)
	commandFlags.StringVar(
		&flags.to,
		"to",
		"",
		"Range end in RFC3339 format (exclusive)",
	)
	commandFlags.StringVar(
		&flags.routerID,
		"router-id",
		"",
		"Limit results to one router ID",
	)
	commandFlags.StringArrayVar(
		&flags.apiKeyIDs,
		"api-key-id",
		nil,
		"Limit results to an API key ID (repeatable)",
	)
	commandFlags.StringArrayVar(
		&flags.userIDs,
		"user-id",
		nil,
		"Limit results to an attributed user ID (repeatable)",
	)
	commandFlags.StringArrayVar(
		&flags.models,
		"model",
		nil,
		"Limit results to a model ID (repeatable)",
	)
	commandFlags.StringVar(
		&flags.provider,
		"provider",
		"",
		"Limit results to one provider",
	)
	commandFlags.StringVar(
		&flags.status,
		"status",
		"",
		"Limit results by status: completed, provider_error, selector_error, or aborted",
	)
	commandFlags.StringVar(
		&flags.organizationID,
		"organization-id",
		"",
		"Read an explicit organization using browser login",
	)
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func activityModelsPath(organizationID string, query url.Values) string {
	return "/v1/organizations/" +
		url.PathEscape(organizationID) +
		"/routing/activity/models?" +
		query.Encode()
}

func (flags activityModelsFlags) query() (url.Values, error) {
	from, err := parseActivityTime("from", flags.from)
	if err != nil {
		return nil, err
	}
	to, err := parseActivityTime("to", flags.to)
	if err != nil {
		return nil, err
	}
	if !to.After(from) {
		return nil, fmt.Errorf("--to must be later than --from")
	}

	status := strings.TrimSpace(flags.status)
	if status != "" && !validActivityStatus(status) {
		return nil, fmt.Errorf(
			"invalid --status %q: expected completed, provider_error, selector_error, or aborted",
			status,
		)
	}

	query := url.Values{}
	query.Set("from", from.Format(time.RFC3339Nano))
	query.Set("to", to.Format(time.RFC3339Nano))
	setOptionalQuery(query, "router_id", flags.routerID)
	setRepeatedQuery(query, "api_key_id", flags.apiKeyIDs)
	setRepeatedQuery(query, "user_id", flags.userIDs)
	setRepeatedQuery(query, "model", flags.models)
	setOptionalQuery(query, "provider", flags.provider)
	setOptionalQuery(query, "status", status)
	return query, nil
}

func parseActivityTime(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --%s: expected RFC3339 timestamp: %w", name, err)
	}
	return parsed, nil
}

func validActivityStatus(value string) bool {
	switch value {
	case "completed", "provider_error", "selector_error", "aborted":
		return true
	default:
		return false
	}
}

func setOptionalQuery(query url.Values, name, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		query.Set(name, trimmed)
	}
}

func setRepeatedQuery(query url.Values, name string, values []string) {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			query.Add(name, trimmed)
		}
	}
}
