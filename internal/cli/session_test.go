package cli

import "testing"

func TestResolveSessionSecretsParsesAssignmentsAndEnv(t *testing.T) {
	t.Setenv("API_TOKEN", "from-env")

	secrets, err := resolveSessionSecrets(
		[]string{"INLINE_TOKEN=inline"},
		[]string{"API_TOKEN"},
	)
	if err != nil {
		t.Fatalf("resolveSessionSecrets returned error: %v", err)
	}
	if secrets["INLINE_TOKEN"] != "inline" {
		t.Fatalf("INLINE_TOKEN = %q", secrets["INLINE_TOKEN"])
	}
	if secrets["API_TOKEN"] != "from-env" {
		t.Fatalf("API_TOKEN = %q", secrets["API_TOKEN"])
	}
}

func TestResolveSessionSecretsRejectsDuplicates(t *testing.T) {
	t.Setenv("API_TOKEN", "from-env")

	_, err := resolveSessionSecrets(
		[]string{"API_TOKEN=inline"},
		[]string{"API_TOKEN"},
	)
	if err == nil {
		t.Fatal("expected duplicate secret error")
	}
}

func TestResolveSessionLLMAPIKeyReadsEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-runtime")

	value, err := resolveSessionLLMAPIKey("", "OPENROUTER_API_KEY")
	if err != nil {
		t.Fatalf("resolveSessionLLMAPIKey returned error: %v", err)
	}
	if value != "sk-runtime" {
		t.Fatalf("llm api key = %q", value)
	}
}

func TestResolveSessionLLMAPIKeyRejectsAmbiguousInput(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-runtime")

	_, err := resolveSessionLLMAPIKey("sk-inline", "OPENROUTER_API_KEY")
	if err == nil {
		t.Fatal("expected ambiguous llm api key error")
	}
}

func TestResolveSessionLLMAPIKeyRejectsMissingEnv(t *testing.T) {
	_, err := resolveSessionLLMAPIKey("", "OPENROUTER_API_KEY")
	if err == nil {
		t.Fatal("expected missing environment variable error")
	}
}
