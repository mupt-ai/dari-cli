package auth

import "testing"

func TestParseManualCallbackURL(t *testing.T) {
	got, err := parseManualCallback("http://127.0.0.1:12345/callback?code=abc123&state=state123")
	if err != nil {
		t.Fatalf("parseManualCallback: %v", err)
	}
	if got.Code != "abc123" || got.State != "state123" || got.Error != "" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestParseManualCallbackRawCodeRejected(t *testing.T) {
	if _, err := parseManualCallback(" abc123 \n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseManualCallbackErrorURL(t *testing.T) {
	got, err := parseManualCallback("http://127.0.0.1:12345/callback?error_description=denied")
	if err != nil {
		t.Fatalf("parseManualCallback: %v", err)
	}
	if got.Error != "denied" || got.Code != "" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestParseManualCallbackURLWithoutCode(t *testing.T) {
	if _, err := parseManualCallback("http://127.0.0.1:12345/callback"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseManualCallbackURLWithoutState(t *testing.T) {
	if _, err := parseManualCallback("http://127.0.0.1:12345/callback?code=abc123"); err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildWebLoginURL(t *testing.T) {
	got, err := buildWebLoginURL(
		"https://app.dari.dev/",
		"http://127.0.0.1:12345/callback",
		"state123",
		"challenge123",
	)
	if err != nil {
		t.Fatalf("buildWebLoginURL: %v", err)
	}
	want := "https://app.dari.dev/login?cli_callback=http%3A%2F%2F127.0.0.1%3A12345%2Fcallback&cli_code_challenge=challenge123&cli_state=state123"
	if got != want {
		t.Fatalf("unexpected URL:\n got: %s\nwant: %s", got, want)
	}
}
