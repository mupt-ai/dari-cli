package cli

import "testing"

func TestClaudeCommandUsesSelectedRouter(t *testing.T) {
	for _, routerID := range []string{"", "default", "rtr_custom"} {
		t.Run(routerID, func(t *testing.T) {
			command, err := buildAgentCommand("/claude", agentLaunch{name: "claude"}, "test-key", routerID)
			if err != nil {
				t.Fatal(err)
			}
			want := routingBaseURL
			if routerID == "rtr_custom" {
				want += "/rtr_custom"
			}
			if got := environmentValue(command.env, "ANTHROPIC_BASE_URL"); got != want {
				t.Fatalf("base URL = %q, want %q", got, want)
			}
		})
	}
}
