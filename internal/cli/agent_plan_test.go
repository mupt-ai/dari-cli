package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/cli/agents"
)

func TestRouterModelsOffersRoutingOnlyWithThePlan(t *testing.T) {
	for _, tc := range []struct {
		allowed    any
		want       agents.Models
		wantNotice bool
	}{
		{true, agents.Models{Routing: true, Enabled: []string{"openai/gpt-5.6-sol", "anthropic/claude-opus-5"}}, false},
		{nil, agents.Models{Routing: true, Enabled: []string{"openai/gpt-5.6-sol", "anthropic/claude-opus-5"}}, false},
		{false, agents.Models{Enabled: []string{"openai/gpt-5.6-sol", "anthropic/claude-opus-5"}}, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/organizations/current/routers":
				_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
					{"id": "rtr_other", "enabled_models": []string{"x/y"}},
					{"id": "rtr_default", "is_default": true, "enabled_models": []string{"openai/gpt-5.6-sol", "anthropic/claude-opus-5"}},
				}})
			case "/v1/organizations/current/credentials":
				_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}, "autorouting_allowed": tc.allowed})
			default:
				t.Fatalf("unexpected request %s", r.URL.Path)
			}
		}))
		var stderr strings.Builder
		got, err := routerModels(t.Context(), api.New(server.URL), "default", &stderr)
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("allowed=%v: models = %+v, want %+v", tc.allowed, got, tc.want)
		}
		if notice := strings.Contains(stderr.String(), "Starting on dari/model/openai/gpt-5.6-sol."); notice != tc.wantNotice {
			t.Errorf("allowed=%v: notice %q", tc.allowed, stderr.String())
		}
	}
}

func TestModelsDefaultAndMenu(t *testing.T) {
	routing := agents.Models{Routing: true, Enabled: []string{"a/b"}}
	if routing.Default() != "dari/routing" || !reflect.DeepEqual(routing.Menu(), []string{"dari/routing", "dari/model/a/b"}) {
		t.Errorf("routing: %s %v", routing.Default(), routing.Menu())
	}
	explicit := agents.Models{Enabled: []string{"a/b", "c/d"}}
	if explicit.Default() != "dari/model/a/b" || !reflect.DeepEqual(explicit.Menu(), []string{"dari/model/a/b", "dari/model/c/d"}) {
		t.Errorf("explicit: %s %v", explicit.Default(), explicit.Menu())
	}
	if (agents.Models{}).Default() != "dari/routing" {
		t.Error("unresolved router should keep dari/routing")
	}
}
