package cli

import "testing"

func TestShouldCreateSessionForSend(t *testing.T) {
	if !shouldCreateSessionForSend([]string{"hello"}, false, "agt_123") {
		t.Fatal("expected --agent and one text argument to auto-create a session")
	}
	if !shouldCreateSessionForSend(nil, true, "agt_123") {
		t.Fatal("expected --agent and --stdin with no args to auto-create a session")
	}
	if shouldCreateSessionForSend([]string{"sess_123"}, true, "agt_123") {
		t.Fatal("expected --agent with session and --stdin to target an existing session")
	}
	if shouldCreateSessionForSend([]string{"sess_123"}, false, "agt_123") {
		t.Fatal("expected a session-looking single arg to target an existing session")
	}
	if shouldCreateSessionForSend([]string{"hello"}, false, "") {
		t.Fatal("expected missing --agent to require an existing session")
	}
}

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
