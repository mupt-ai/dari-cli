package cli

import "testing"

func TestMatchAgentRefResolvesUniqueName(t *testing.T) {
	id, err := matchAgentRef("support-agent", []agentReferenceSummary{
		{ID: "agt_1", Name: "other"},
		{ID: "agt_2", Name: "support-agent"},
	})
	if err != nil {
		t.Fatalf("matchAgentRef returned error: %v", err)
	}
	if id != "agt_2" {
		t.Fatalf("agent id = %q", id)
	}
}

func TestMatchAgentRefRejectsAmbiguousName(t *testing.T) {
	_, err := matchAgentRef("support-agent", []agentReferenceSummary{
		{ID: "agt_1", Name: "support-agent"},
		{ID: "agt_2", Name: "support-agent"},
	})
	if err == nil {
		t.Fatal("expected ambiguous agent name error")
	}
}

func TestMatchSessionRefResolvesUniqueName(t *testing.T) {
	name := "debug-run"
	id, err := matchSessionRef("debug-run", []sessionReferenceSummary{
		{ID: "sess_1"},
		{ID: "sess_2", Name: &name},
	}, "")
	if err != nil {
		t.Fatalf("matchSessionRef returned error: %v", err)
	}
	if id != "sess_2" {
		t.Fatalf("session id = %q", id)
	}
}

func TestMatchSessionRefRejectsAmbiguousName(t *testing.T) {
	name := "debug-run"
	_, err := matchSessionRef("debug-run", []sessionReferenceSummary{
		{ID: "sess_1", Name: &name},
		{ID: "sess_2", Name: &name},
	}, "")
	if err == nil {
		t.Fatal("expected ambiguous session name error")
	}
}
