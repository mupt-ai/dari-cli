package auth

import "testing"

func TestParseManualCallbackURL(t *testing.T) {
	got, err := parseManualCallback("http://127.0.0.1:12345/callback?code=abc123")
	if err != nil {
		t.Fatalf("parseManualCallback: %v", err)
	}
	if got.Code != "abc123" || got.Error != "" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestParseManualCallbackRawCode(t *testing.T) {
	got, err := parseManualCallback(" abc123 \n")
	if err != nil {
		t.Fatalf("parseManualCallback: %v", err)
	}
	if got.Code != "abc123" || got.Error != "" {
		t.Fatalf("unexpected result: %+v", got)
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
