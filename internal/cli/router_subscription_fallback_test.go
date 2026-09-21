package cli

import (
	"encoding/json"
	"testing"
)

func TestSubscriptionFallbackModelFlagParsesAndClears(t *testing.T) {
	rf := routerConfigFlags{subscriptionFallbackModels: []string{
		"anthropic/claude-opus-5=openai/gpt-5.6-terra,via=OpenRouter,thinking=High,fast",
		"openai/gpt-5.6-sol=",
	}}
	fallbacks, err := rf.subscriptionFallbacks()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(routerUpdateRequest{SubscriptionFallbackModels: fallbacks})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SubscriptionFallbackModels map[string]*routerSubscriptionFallback `json:"subscription_fallback_models"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	anthropic := decoded.SubscriptionFallbackModels["anthropic/claude-opus-5"]
	if anthropic == nil || anthropic.Model != "openai/gpt-5.6-terra" || anthropic.Provider != "openrouter" || anthropic.ThinkingLevel != "high" || !anthropic.FastMode {
		t.Fatalf("lost the per-model fallback settings: %s", body)
	}
	if value, present := decoded.SubscriptionFallbackModels["openai/gpt-5.6-sol"]; !present || value != nil {
		t.Fatalf("a bare source model must send null to clear the entry: %s", body)
	}

	bare, err := (&routerConfigFlags{subscriptionFallbackModels: []string{"openai/gpt-5.6-sol=anthropic/claude-sonnet-5"}}).subscriptionFallbacks()
	if err != nil {
		t.Fatal(err)
	}
	if got := bare["openai/gpt-5.6-sol"]; got == nil || got.Model != "anthropic/claude-sonnet-5" || got.Provider != "" || got.ThinkingLevel != "" || got.FastMode {
		t.Fatalf("a bare fallback model must leave provider, thinking level, and Fast Mode to the server defaults: %+v", got)
	}

	if _, err := (&routerConfigFlags{subscriptionFallbackModels: []string{"openai/gpt-5.6-terra"}}).subscriptionFallbacks(); err == nil {
		t.Fatal("a pair without a source model must be rejected")
	}
	if _, err := (&routerConfigFlags{subscriptionFallbackModels: []string{"openai/gpt-5.6-sol=anthropic/claude-sonnet-5,speed=fast"}}).subscriptionFallbacks(); err == nil {
		t.Fatal("an unknown option must be rejected")
	}

	body, err = json.Marshal(routerUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["subscription_fallback_models"]; present {
		t.Fatal("omission must preserve the current fallbacks")
	}
}
