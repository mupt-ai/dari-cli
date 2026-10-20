package agents

import (
	"net/http"
	"testing"
	"time"
)

func TestStatusFromHeaders(t *testing.T) {
	for _, tc := range []struct{ remaining, thinking, want string }{
		{"0", "off", "test/model · lease 1 turn left · cache cold"},
		{"1", "high", "test/model · high · lease 2 turns left · cache cold"},
		{"", "", "test/model · cache cold"},
		{"bad", "high", "test/model · high · cache cold"},
		{"-1", "off", "test/model · cache cold"},
		{"1000000", "off", "test/model · cache cold"},
	} {
		headers := http.Header{}
		headers.Set("x-dari-selected-model", "test/model")
		headers.Set("x-dari-thinking-level", tc.thinking)
		headers.Set("x-dari-lease-turns-remaining", tc.remaining)
		if got := StatusFromHeaders(headers).String(); got != tc.want {
			t.Errorf("%+v: got %q", tc, got)
		}
	}
	if got := (Status{Thinking: "high"}).String(); got != "" {
		t.Fatal(got)
	}
	if got := (Status{Model: "test\n/model\x1b‮"}).String(); got != "test/model · cache cold" {
		t.Fatal(got)
	}
}

func TestStatusCache(t *testing.T) {
	cached := func(tokens int64) *int64 { return &tokens }
	now := time.Now()
	for _, tc := range []struct {
		status Status
		want   string
	}{
		{Status{Model: "m"}, "m · cache cold"},
		{Status{Model: "m", CachedTokens: cached(1), RespondedAt: now}, "m · cache warm"},
		{Status{Model: "m", CachedTokens: cached(0), RespondedAt: now}, "m · cache cold"},
		{Status{Model: "m", CachedTokens: cached(1), RespondedAt: now.Add(-CacheTTL)}, "m · cache cold"},
		{Status{Model: "m", CachedTokens: cached(1)}, "m · cache cold"},
		{Status{CachedTokens: cached(1), RespondedAt: now}, ""},
	} {
		if got := tc.status.String(); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.status, got, tc.want)
		}
	}
}
