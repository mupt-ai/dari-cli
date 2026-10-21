package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
)

// A self-hosted Dari Gateway serves the routing and subscription paths of the
// hosted API. DARI_GATEWAY_URL points the agent launchers and subscription
// commands at it, and then they make no hosted control-plane calls.
const (
	gatewayURLEnv = "DARI_GATEWAY_URL"
	gatewayKeyEnv = "DARI_GATEWAY_KEY"
)

type gatewayConfig struct {
	url string
	key string
}

// gatewayFromEnv returns nil when DARI_GATEWAY_URL is unset, which keeps the
// hosted behavior.
func gatewayFromEnv() (*gatewayConfig, error) {
	raw := strings.TrimSpace(os.Getenv(gatewayURLEnv))
	if raw == "" {
		return nil, nil
	}
	base := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(raw, "/"), "/v1"), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("%s must be an http or https URL", gatewayURLEnv)
	}
	key := strings.TrimSpace(os.Getenv(gatewayKeyEnv))
	if key == "" {
		return nil, fmt.Errorf("%s is required when %s is set", gatewayKeyEnv, gatewayURLEnv)
	}
	return &gatewayConfig{url: base, key: key}, nil
}

// subscriptionClient authenticates subscription commands with the gateway key
// in gateway mode, and otherwise with the current organization's key.
func subscriptionClient(gf *globalFlags) (*api.Client, error) {
	gateway, err := gatewayFromEnv()
	if err != nil {
		return nil, err
	}
	if gateway != nil {
		return api.New(gateway.url).WithBearer(gateway.key), nil
	}
	apiURL, err := gf.resolveAPIURL()
	if err != nil {
		return nil, err
	}
	return auth.OrgKeyClient(apiURL)
}

// subscriptionRequest is orgKeyRequest for the subscription commands, which
// talk to the gateway in gateway mode.
func subscriptionRequest(cmd *cobra.Command, gf *globalFlags, method, path string, body, out any) error {
	client, err := subscriptionClient(gf)
	if err != nil {
		return err
	}
	return client.Do(cmd.Context(), method, path, body, out)
}
