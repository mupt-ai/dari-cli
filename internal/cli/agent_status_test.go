package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestRoutingStatus(t *testing.T) {
	for _, tc := range []struct{ remaining, thinking, want string }{
		{"0", "off", "test/model · lease 1 turn left"},
		{"1", "high", "test/model · high · lease 2 turns left"},
		{"", "", "test/model"},
		{"bad", "high", "test/model · high"},
		{"-1", "off", "test/model"},
		{"1000000", "off", "test/model"},
	} {
		headers := http.Header{}
		headers.Set("x-dari-selected-model", "test/model")
		headers.Set("x-dari-thinking-level", tc.thinking)
		headers.Set("x-dari-lease-turns-remaining", tc.remaining)
		if got := routingStatusFromHeaders(headers).String(); got != tc.want {
			t.Errorf("%+v: got %q", tc, got)
		}
		root := newRootCmd("test")
		root.SetArgs([]string{"agent-status", "--headers", `{"x-dari-selected-model":"test/model","x-dari-thinking-level":"` + tc.thinking + `","x-dari-lease-turns-remaining":"` + tc.remaining + `"}`})
		var output strings.Builder
		root.SetOut(&output)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(output.String()) != tc.want {
			t.Fatal(output.String())
		}
	}
	if got := (routingStatus{Thinking: "high"}).String(); got != "" {
		t.Fatal(got)
	}
	if got := (routingStatus{Model: "test\n/model\x1b\u202e"}).String(); got != "test/model" {
		t.Fatal(got)
	}
}

func TestCodexLaunchClassification(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"fix tests"}, true},
		{[]string{"-c", "model=\"exec\"", "resume", "--last"}, true},
		{[]string{"--model", "review", "exec", "hello"}, false},
		{[]string{"-C", "exec", "fork", "--last"}, true},
		{[]string{"--", "exec"}, true},
		{[]string{"-c", "model=\"hello\"", "exec", "hello"}, false},
		{[]string{"mcp", "list"}, false},
		{[]string{"--config=model=\"hello\"", "review"}, false},
	} {
		if got := codexInteractiveArgs(tc.args); got != tc.want {
			t.Errorf("%q: got %v", tc.args, got)
		}
	}
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"codex-cli 0.162.1\n", true}, {"codex-cli 0.163.0", true},
		{"codex-cli 0.162.0", false}, {"codex-cli 0.144.0", false}, {"unknown", false},
	} {
		if codexSupportsStatusBridge(tc.version) != tc.want {
			t.Errorf("%s", tc.version)
		}
	}
}
