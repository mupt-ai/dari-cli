package cli

import (
	"encoding/json"
	"testing"
)

func TestLongContextFlagRequiresExplicitOptIn(t *testing.T) {
	command := newRouterUpdateCmd(&globalFlags{})
	flag := command.Flags().Lookup("allow-long-context")
	if flag == nil || flag.DefValue != "false" {
		t.Fatal("long context must default off")
	}
	for _, enabled := range []bool{true, false} {
		body, err := json.Marshal(routerUpdateRequest{AllowLongContext: &enabled})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["allow_long_context"] != enabled {
			t.Fatalf("lost explicit value: %s", body)
		}
	}
	body, err := json.Marshal(routerUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, present := decoded["allow_long_context"]; present {
		t.Fatal("omission must preserve the current setting")
	}
}
