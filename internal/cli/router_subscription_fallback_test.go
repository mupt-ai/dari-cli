package cli

import (
	"encoding/json"
	"testing"
)

func TestSubscriptionFallbackModelFlagParsesAndClears(t *testing.T) {
	rf := routerConfigFlags{subscriptionFallbackModels: []string{
		"Anthropic=openai/gpt-5.6-terra,via=OpenRouter,thinking=High,fast",
		"openai=",
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
	anthropic := decoded.SubscriptionFallbackModels["anthropic"]
	if anthropic == nil || anthropic.Model != "openai/gpt-5.6-terra" || anthropic.Provider != "openrouter" || anthropic.ThinkingLevel != "high" || !anthropic.FastMode {
		t.Fatalf("lost the anthropic fallback settings: %s", body)
	}
	if value, present := decoded.SubscriptionFallbackModels["openai"]; !present || value != nil {
		t.Fatalf("a bare provider= must send null to clear the entry: %s", body)
	}

	bare, err := (&routerConfigFlags{subscriptionFallbackModels: []string{"openai=anthropic/claude-sonnet-5"}}).subscriptionFallbacks()
	if err != nil {
		t.Fatal(err)
	}
	if got := bare["openai"]; got == nil || got.Model != "anthropic/claude-sonnet-5" || got.Provider != "" || got.ThinkingLevel != "" || got.FastMode {
		t.Fatalf("a bare MODEL_ID must leave provider, thinking level, and Fast Mode to the server defaults: %+v", got)
	}

	if _, err := (&routerConfigFlags{subscriptionFallbackModels: []string{"openai/gpt-5.6-terra"}}).subscriptionFallbacks(); err == nil {
		t.Fatal("a pair without a provider must be rejected")
	}
	if _, err := (&routerConfigFlags{subscriptionFallbackModels: []string{"openai=anthropic/claude-sonnet-5,speed=fast"}}).subscriptionFallbacks(); err == nil {
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
