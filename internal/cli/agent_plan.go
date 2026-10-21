package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/cli/agents"
)

type agentSubscriptionStatus struct {
	Credentials        []agentCredential `json:"credentials"`
	AutoroutingAllowed *bool             `json:"autorouting_allowed"`
}

// agentModels builds the model menu for a launch: every model enabled on the
// router, plus dari/routing when the organization has the Autorouting plan.
// Without the plan the launch starts on the router's first model and says so.
func agentModels(ctx context.Context, access agentRoutingAccess, routerID string, stderr io.Writer) (agents.Models, error) {
	client, err := auth.OrgKeyClient(access.apiURL)
	if err != nil {
		return agents.Models{}, err
	}
	return routerModels(ctx, client, routerID, stderr)
}

func routerModels(ctx context.Context, client *api.Client, routerID string, stderr io.Writer) (agents.Models, error) {
	routers, err := listAgentRouters(ctx, client)
	if err != nil {
		return agents.Models{}, err
	}
	router, ok := findAgentRouter(routers, func(router agentRouter) bool {
		if routerID == "default" {
			return router.IsDefault
		}
		return router.ID == routerID
	})
	if !ok {
		return agents.Models{Routing: true}, nil
	}
	var status agentSubscriptionStatus
	if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, &status); err != nil {
		return agents.Models{}, api.HumanError(err)
	}
	models := agents.Models{
		// Older servers do not report the plan; they route as before.
		Routing: status.AutoroutingAllowed == nil || *status.AutoroutingAllowed,
		Enabled: router.EnabledModels,
	}
	if !models.Routing && len(models.Enabled) > 0 {
		fmt.Fprintf(stderr, "Starting on %s. Switch models with /model. Autorouting (%s) needs the Autorouting plan; subscribe on the Usage page.\n", models.Default(), agents.RoutingModel)
	}
	return models, nil
}
