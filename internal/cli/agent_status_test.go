package cli

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mupt-ai/dari-cli/internal/cli/agents"
)

func TestAgentStatusCommand(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--headers", `{"x-dari-selected-model":"m","x-dari-thinking-level":"high","x-dari-lease-turns-remaining":"1"}`}, "m · high · lease 2 turns left · cache cold"},
		{[]string{"--headers", `{"x-dari-selected-model":"m"}`, "--cached-tokens", "10", "--responded-at", strconv.FormatInt(now.UnixMilli(), 10)}, "m · cache warm"},
		{[]string{"--headers", `{"x-dari-selected-model":"m"}`, "--cached-tokens", "0", "--responded-at", strconv.FormatInt(now.UnixMilli(), 10)}, "m · cache cold"},
		{[]string{"--headers", `{"x-dari-selected-model":"m"}`, "--cached-tokens", "10", "--responded-at", strconv.FormatInt(now.Add(-agents.CacheTTL).UnixMilli(), 10)}, "m · cache cold"},
		{[]string{"--headers", `{"x-dari-thinking-level":"high"}`}, ""},
	} {
		root := newRootCmd("test")
		root.SetArgs(append([]string{"agent-status"}, tc.args...))
		var output strings.Builder
		root.SetOut(&output)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(output.String()); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.args, got, tc.want)
		}
	}
}
