package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/state"
)

func TestSelectAgentModelsByNumberKeepsDefaultsOnEnter(t *testing.T) {
	models := testAgentModels()
	defaults := []string{"anthropic/claude-fable-5", "zai-org/GLM-5.3"}
	var output strings.Builder

	got, err := selectAgentModelsByNumber(strings.NewReader("\n"), &output, "Configure", models, defaults, nil, testRecommendedLevels())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(choiceIDs(got), defaults) {
		t.Fatalf("selected = %q, want %q", choiceIDs(got), defaults)
	}
	if !strings.Contains(output.String(), "[x] Fable") || !strings.Contains(output.String(), "[x] GLM") {
		t.Fatalf("output does not mark defaults as selected:\n%s", output.String())
	}
}

func TestSelectAgentModelsByNumberAcceptsConfiguredList(t *testing.T) {
	got, err := selectAgentModelsByNumber(
		strings.NewReader("3, 1, 3\n"),
		io.Discard,
		"Configure",
		testAgentModels(),
		[]string{"anthropic/claude-fable-5"},
		nil,
		testRecommendedLevels(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"openai/gpt-5.6-sol", "anthropic/claude-fable-5"}
	if !slices.Equal(choiceIDs(got), want) {
		t.Fatalf("selected = %q, want %q", choiceIDs(got), want)
	}
}

func TestSelectAgentModelsByNumberRejectsInvalidSelection(t *testing.T) {
	_, err := selectAgentModelsByNumber(
		strings.NewReader("9\n"),
		io.Discard,
		"Configure",
		testAgentModels(),
		nil,
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "invalid model number") {
		t.Fatalf("error = %v", err)
	}
}

func TestSelectAgentModelsByNumberLeavesUnconsumedStdinForAgent(t *testing.T) {
	stdin := strings.NewReader("\nleftover agent input")
	_, err := selectAgentModelsByNumber(stdin, io.Discard, "Configure", testAgentModels(), []string{"anthropic/claude-fable-5"}, nil, testRecommendedLevels())
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := io.ReadAll(stdin)
	if err != nil {
		t.Fatal(err)
	}
	if string(remaining) != "leftover agent input" {
		t.Fatalf("unconsumed stdin = %q", remaining)
	}
}

func TestSelectAgentModelsByNumberPreservesCurrentLevels(t *testing.T) {
	models := testAgentModels()
	current := map[string][]string{"openai/gpt-5.6-sol": {"high"}}
	got, err := selectAgentModelsByNumber(
		strings.NewReader("3\n"),
		io.Discard,
		"Configure",
		models,
		nil,
		current,
		testRecommendedLevels(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "openai/gpt-5.6-sol" {
		t.Fatalf("choices = %#v", got)
	}
	if !slices.Equal(got[0].Levels, []string{"high"}) {
		t.Fatalf("levels = %q, want current [high]", got[0].Levels)
	}
}

func TestAgentPickerTogglesModelsAndEditsLevels(t *testing.T) {
	models := testAgentModels()
	models[2].SupportedLevels = []string{"off", "low", "medium", "high", "xhigh", "max"}
	p := newAgentPickerState(models, []string{"anthropic/claude-fable-5", "zai-org/GLM-5.3"}, nil, testRecommendedLevels())

	// Sol starts with the recommended medium+xhigh set checked.
	if got := p.rows[2].checkedLevels(); !slices.Equal(got, []string{"medium", "xhigh"}) {
		t.Fatalf("Sol initial levels = %q", got)
	}
	if !p.rows[2].usesRecommended() {
		t.Fatal("Sol does not report using the recommended set")
	}

	// Move down to Sol, enable then disable it.
	p.handleKey("down")
	p.handleKey("down")
	if p.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", p.cursor)
	}
	p.handleKey(" ")
	if !p.rows[2].selected {
		t.Fatal("Sol is not selected after space")
	}
	p.handleKey(" ")
	if p.rows[2].selected {
		t.Fatal("Sol is still selected after second space")
	}

	// Open level editing: cursor starts on the first checked level.
	p.handleKey("right")
	if !p.editing || p.levelCursor != 2 {
		t.Fatalf("editing = %v levelCursor = %d, want true 2 (medium)", p.editing, p.levelCursor)
	}

	// Check an extra level: medium -> down -> high -> space.
	p.handleKey("down")
	p.handleKey(" ")
	if got := p.rows[2].checkedLevels(); !slices.Equal(got, []string{"medium", "high", "xhigh"}) {
		t.Fatalf("levels = %q, want [medium high xhigh]", got)
	}
	if p.rows[2].usesRecommended() {
		t.Fatal("mixed levels still marked as recommended")
	}

	// Back on the model list, confirm.
	p.handleKey("left")
	if p.editing {
		t.Fatal("still editing after left")
	}
	done, canceled := p.handleKey("enter")
	if !done || canceled {
		t.Fatalf("enter = done %v canceled %v", done, canceled)
	}
	choices := p.choices()
	if len(choices) != 2 {
		t.Fatalf("choices = %#v", choices)
	}
	if !slices.Equal(choices[0].Levels, []string{"high"}) {
		t.Fatalf("Fable levels = %q", choices[0].Levels)
	}
}

func TestAgentPickerRecommendationsAreAgentSpecific(t *testing.T) {
	models := testAgentModels()
	codex := newAgentPickerState(
		models,
		[]string{"openai/gpt-5.6-sol"},
		nil,
		map[string][]string{"openai/gpt-5.6-sol": {"medium", "xhigh"}},
	)
	if codex.rows[0].usesRecommended() {
		t.Fatal("Codex marked Fable as recommended")
	}
	if got := codex.rows[0].checkedLevels(); !slices.Equal(got, []string{"high"}) {
		t.Fatalf("Codex Fable levels = %q, want Dari default [high]", got)
	}
	if !codex.rows[2].usesRecommended() {
		t.Fatal("Codex did not mark Sol as recommended")
	}

	claude := newAgentPickerState(
		models,
		[]string{"anthropic/claude-fable-5", "zai-org/GLM-5.3"},
		nil,
		map[string][]string{
			"anthropic/claude-fable-5": {"high"},
			"zai-org/GLM-5.3":          {"high"},
		},
	)
	if !claude.rows[0].usesRecommended() {
		t.Fatal("Claude Code did not mark Fable as recommended")
	}
	if claude.rows[2].usesRecommended() {
		t.Fatal("Claude Code marked Sol as recommended")
	}
}

func TestAgentPickerLevelEditingRestoresRecommendedWhenEmptied(t *testing.T) {
	models := testAgentModels()
	models[2].SupportedLevels = []string{"off", "low", "medium", "high", "xhigh", "max"}
	p := newAgentPickerState(models, []string{"openai/gpt-5.6-sol"}, nil, testRecommendedLevels())

	// Open Sol's levels and uncheck everything.
	p.handleKey("down")
	p.handleKey("down")
	p.handleKey("right")
	p.handleKey(" ")    // uncheck medium
	p.handleKey("down") // high
	p.handleKey("down") // xhigh
	p.handleKey(" ")    // uncheck xhigh
	if got := p.rows[2].checkedLevels(); len(got) != 0 {
		t.Fatalf("levels = %q, want none", got)
	}

	// Leaving level editing falls back to the recommended set.
	p.handleKey("left")
	if got := p.rows[2].checkedLevels(); !slices.Equal(got, []string{"medium", "xhigh"}) {
		t.Fatalf("levels after exit = %q, want recommended [medium xhigh]", got)
	}
}

func TestAgentPickerKeyParsing(t *testing.T) {
	got := parseAgentPickerKeys([]byte("\x1b[B\x1b[C j\r"))
	want := []string{"down", "right", " ", "j", "enter"}
	if !slices.Equal(got, want) {
		t.Fatalf("keys = %q, want %q", got, want)
	}
	if got := parseAgentPickerKeys([]byte("\x03")); !slices.Equal(got, []string{"quit"}) {
		t.Fatalf("ctrl-c = %q, want [quit]", got)
	}
	if got := parseAgentPickerKeys([]byte("\x1b")); !slices.Equal(got, []string{"esc"}) {
		t.Fatalf("esc = %q, want [esc]", got)
	}
}

func TestAgentPickerRowKeepsCustomCurrentLevels(t *testing.T) {
	model := agentModel{
		ID:              "zai-org/GLM-5.3",
		SupportedLevels: []string{"low", "high", "max"},
	}
	row := newAgentPickerRow(model, []string{"max"}, []string{"high"})
	if got := row.checkedLevels(); !slices.Equal(got, []string{"max"}) {
		t.Fatalf("levels = %q, want current [max]", got)
	}
	if row.usesRecommended() {
		t.Fatal("custom levels reported as recommended")
	}

	// A current level outside the supported list stays selectable.
	row = newAgentPickerRow(model, []string{"medium"}, []string{"high"})
	if !slices.Contains(row.levelOpts, "medium") {
		t.Fatalf("level options = %q, want to include current medium", row.levelOpts)
	}
	if got := row.checkedLevels(); !slices.Equal(got, []string{"medium"}) {
		t.Fatalf("levels = %q, want [medium]", got)
	}
}

func TestAgentRouterCreateBodyUsesCurrentDefaultEvals(t *testing.T) {
	body := agentRouterCreateBody(testAgentModels(), []agentModelChoice{
		{ID: "anthropic/claude-fable-5", Levels: []string{"high"}},
		{ID: "zai-org/GLM-5.3", Levels: []string{"high"}},
	}, claudeRouterClientKey, true)
	if !slices.Equal(body.EvalIDs, defaultAgentEvalIDs) {
		t.Fatalf("eval_ids = %q, want %q", body.EvalIDs, defaultAgentEvalIDs)
	}
	if !body.PersonalOAuthEnabled {
		t.Error("personal OAuth is disabled")
	}
	if body.PersonalOAuthFallbackEnabled {
		t.Error("personal OAuth fallback is enabled")
	}
	if !slices.Equal(body.EnabledModels, []string{"anthropic/claude-fable-5", "zai-org/GLM-5.3"}) {
		t.Errorf("enabled_models = %q", body.EnabledModels)
	}
	withoutSubscription := agentRouterCreateBody(testAgentModels(), []agentModelChoice{
		{ID: "anthropic/claude-fable-5", Levels: []string{"high"}},
	}, claudeManagedRouterClientKey, false)
	if withoutSubscription.PersonalOAuthEnabled {
		t.Error("personal OAuth is enabled after opt-out")
	}
	if withoutSubscription.ClientKey != claudeManagedRouterClientKey {
		t.Errorf("managed client key = %q", withoutSubscription.ClientKey)
	}
	if withoutSubscription.Name != claudeManagedRouterName {
		t.Errorf("managed router name = %q", withoutSubscription.Name)
	}
	if !slices.Equal(body.ModelThinkingLevels["anthropic/claude-fable-5"], []string{"high"}) {
		t.Errorf("Fable levels = %q", body.ModelThinkingLevels["anthropic/claude-fable-5"])
	}
}

func TestBuildClaudeAgentCommandTargetsAgentRouter(t *testing.T) {
	command, err := buildAgentCommand(
		"/claude",
		agentLaunch{name: "claude"},
		"dari_route",
		"rtr_claude",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := environmentValue(command.env, "ANTHROPIC_BASE_URL"); got != routingBaseURL+"/rtr_claude" {
		t.Fatalf("ANTHROPIC_BASE_URL = %q", got)
	}
}

func TestClaudeSubscriptionPreferenceScopeIncludesLoggedInUser(t *testing.T) {
	t.Setenv("DARI_CONFIG_DIR", t.TempDir())
	if err := state.Save(&state.CliState{
		SupabaseSession: &state.SupabaseSession{UserID: "user_one"},
		Organizations:   map[string]state.Organization{},
	}); err != nil {
		t.Fatal(err)
	}

	scope, err := claudeSubscriptionPreferenceScope("https://api.example.test|org:org_one")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.example.test|org:org_one|agent:claude|user:user_one"
	if scope != want {
		t.Fatalf("scope = %q, want %q", scope, want)
	}
}

func TestClaudeSubscriptionPreferenceScopeKeepsAPIKeyScope(t *testing.T) {
	t.Setenv("DARI_CONFIG_DIR", t.TempDir())
	scope, err := claudeSubscriptionPreferenceScope("https://api.example.test|key:abc")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.example.test|key:abc|agent:claude"
	if scope != want {
		t.Fatalf("scope = %q, want %q", scope, want)
	}
}

func TestResolveClaudePersonalSubscriptionUsesExistingConnection(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/current/credentials" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeClaudeSubscriptionCredential(w)
	}))
	defer server.Close()

	var stderr strings.Builder
	resolution, err := resolveClaudePersonalSubscription(
		context.Background(),
		api.New(server.URL),
		strings.NewReader("n\n"),
		&stderr,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.enabled || !resolution.decided || !resolution.preferenceChanged {
		t.Fatalf("resolution = %#v, want enabled with cleared opt-out", resolution)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected prompt: %q", stderr.String())
	}
}

func TestResolveClaudePersonalSubscriptionDefaultsToConnect(t *testing.T) {
	var completed map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/current/credentials/oauth/sessions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if got := body["provider"]; got != "anthropic_claude_code" {
				t.Errorf("provider = %#v", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_token":     "oauth_session",
				"authorization_url": "https://claude.example.test/authorize",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/current/credentials/oauth/sessions/complete":
			if err := json.NewDecoder(r.Body).Decode(&completed); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	originalOpenBrowser := openAgentBrowser
	openAgentBrowser = func(string) bool { return false }
	t.Cleanup(func() { openAgentBrowser = originalOpenBrowser })

	var stderr strings.Builder
	resolution, err := resolveClaudePersonalSubscription(
		context.Background(),
		api.New(server.URL),
		strings.NewReader("\nhttp://localhost:53692/callback?code=test&state=test\n"),
		&stderr,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.enabled || !resolution.decided || !resolution.preferenceChanged {
		t.Fatalf("resolution = %#v, want enabled from default answer", resolution)
	}
	if got := completed["session_token"]; got != "oauth_session" {
		t.Errorf("session_token = %#v", got)
	}
	if got := completed["authorization_response"]; got != "http://localhost:53692/callback?code=test&state=test" {
		t.Errorf("authorization_response = %#v", got)
	}
	for _, text := range []string{
		"Use your Claude Code personal subscription? [Y/n]",
		"Open this URL to connect your Claude Code subscription",
		"Connected your Claude Code personal subscription.",
	} {
		if !strings.Contains(stderr.String(), text) {
			t.Errorf("stderr missing %q:\n%s", text, stderr.String())
		}
	}
}

func TestResolveClaudePersonalSubscriptionCompletesBrowserCallback(t *testing.T) {
	var completed map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		case "/v1/organizations/current/credentials/oauth/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_token":     "oauth_session",
				"authorization_url": "https://claude.example.test/authorize",
			})
		case "/v1/organizations/current/credentials/oauth/sessions/complete":
			if err := json.NewDecoder(r.Body).Decode(&completed); err != nil {
				t.Error(err)
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	originalOpenBrowser := openAgentBrowser
	openAgentBrowser = func(string) bool {
		response, err := http.Get("http://" + claudeOAuthCallbackAddress + "/callback?code=test&state=test")
		if err != nil {
			t.Error(err)
			return false
		}
		response.Body.Close()
		return true
	}
	t.Cleanup(func() { openAgentBrowser = originalOpenBrowser })

	resolution, err := resolveClaudePersonalSubscription(
		context.Background(),
		api.New(server.URL),
		strings.NewReader("\n"),
		io.Discard,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.enabled {
		t.Fatal("browser callback did not enable personal subscriptions")
	}
	if got := completed["authorization_response"]; got != "http://localhost:53692/callback?code=test&state=test" {
		t.Errorf("authorization_response = %#v", got)
	}
}

func TestResolveClaudePersonalSubscriptionAllowsImmediatePastedCallback(t *testing.T) {
	var completed map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		case "/v1/organizations/current/credentials/oauth/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_token":     "oauth_session",
				"authorization_url": "https://claude.example.test/authorize",
			})
		case "/v1/organizations/current/credentials/oauth/sessions/complete":
			if err := json.NewDecoder(r.Body).Decode(&completed); err != nil {
				t.Error(err)
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	originalOpenBrowser := openAgentBrowser
	openAgentBrowser = func(string) bool { return true }
	t.Cleanup(func() { openAgentBrowser = originalOpenBrowser })

	var stderr strings.Builder
	resolution, err := resolveClaudePersonalSubscription(
		context.Background(),
		api.New(server.URL),
		strings.NewReader("\nhttp://localhost:53692/callback?code=pasted&state=test\n"),
		&stderr,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.enabled {
		t.Fatal("pasted callback did not enable personal subscriptions")
	}
	if got := completed["authorization_response"]; got != "http://localhost:53692/callback?code=pasted&state=test" {
		t.Errorf("authorization_response = %#v", got)
	}
	if !strings.Contains(stderr.String(), "Paste localhost callback URL") {
		t.Fatalf("stderr does not offer immediate paste input:\n%s", stderr.String())
	}
}

func TestClaudeOAuthTUIUsesAgentPickerStyles(t *testing.T) {
	output := strings.Join(claudeOAuthLines("https://claude.example.test/authorize", true, 80), "\n")
	output += strings.Join(agentChoiceLines(
		agentBannerLines("Connect Claude Code"),
		"Complete Authorization",
		[]agentChoiceOption{
			{label: "Wait for automatic callback", note: "same-machine browser"},
			{label: "Paste localhost callback URL", note: "remote browser"},
		},
		1,
	), "\n")

	for _, text := range []string{
		dariBanner,
		"Connect Claude Code",
		ansiCoral,
		ansiCyan,
		ansiGreen,
		"Anthropic login opened in your browser",
		"Paste localhost callback URL",
	} {
		if !strings.Contains(output, text) {
			t.Errorf("styled OAuth output missing %q:\n%s", text, output)
		}
	}
}

func TestClaudeCallbackInputIgnoresSplitTerminalEscapes(t *testing.T) {
	input := claudeCallbackInputState{}
	chunks := [][]byte{
		[]byte("\x1b["),
		[]byte("A\x1b[20"),
		[]byte("0~http://localhost:53692/callback?code=test&state=test\x1b[2"),
		[]byte("01~\r"),
	}
	var submitted, canceled bool
	for _, chunk := range chunks {
		submitted, canceled = input.update(chunk)
	}
	if !submitted || canceled {
		t.Fatalf("submitted = %t, canceled = %t", submitted, canceled)
	}
	if want := "http://localhost:53692/callback?code=test&state=test"; input.value != want {
		t.Fatalf("input = %q, want %q", input.value, want)
	}
}

func TestClaudeCallbackInputCtrlCCancels(t *testing.T) {
	input := claudeCallbackInputState{value: "partial"}
	submitted, canceled := input.update([]byte{0x03})
	if input.value != "partial" || submitted || !canceled {
		t.Fatalf("input = %q, submitted = %t, canceled = %t", input.value, submitted, canceled)
	}
}

func TestResolveClaudePersonalSubscriptionDoesNotDefaultOnEOF(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/current/credentials" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
	}))
	defer server.Close()

	resolution, err := resolveClaudePersonalSubscription(
		context.Background(),
		api.New(server.URL),
		strings.NewReader(""),
		io.Discard,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.enabled || resolution.decided || resolution.preferenceChanged {
		t.Fatalf("resolution = %#v, want no decision on EOF", resolution)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want credentials request only", requests)
	}
}

func TestResolveClaudePersonalSubscriptionPreservesOptOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/current/credentials" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
	}))
	defer server.Close()

	var stderr strings.Builder
	resolution, err := resolveClaudePersonalSubscription(
		context.Background(),
		api.New(server.URL),
		strings.NewReader("\n"),
		&stderr,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.enabled || !resolution.decided || resolution.preferenceChanged {
		t.Fatalf("resolution = %#v, want persisted opt-out", resolution)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected prompt after persisted opt-out: %q", stderr.String())
	}
}

func TestResolveClaudePersonalSubscriptionAllowsOptOut(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/current/credentials" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
	}))
	defer server.Close()

	resolution, err := resolveClaudePersonalSubscription(
		context.Background(),
		api.New(server.URL),
		strings.NewReader("n\n"),
		io.Discard,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.enabled || !resolution.decided || !resolution.preferenceChanged {
		t.Fatalf("resolution = %#v, want saved opt-out", resolution)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestEnsureClaudeAgentRouterCreatesSeparateRouter(t *testing.T) {
	var created map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer dari_test" {
			t.Errorf("Authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
				{
					"id":                    "rtr_default",
					"name":                  "Default",
					"is_default":            true,
					"enabled_models":        []string{"openai/gpt-5.6-sol"},
					"model_thinking_levels": map[string][]string{"openai/gpt-5.6-sol": {"medium", "xhigh"}},
				},
				{
					"id":   "rtr_same_name",
					"name": claudeRouterName,
				},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			writeClaudeSubscriptionCredential(w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers/model-catalog":
			writeAgentModelCatalog(t, w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/current/routers":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_claude", "name": claudeRouterName})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	t.Setenv("DARI_API_URL", server.URL)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader("\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_claude" {
		t.Fatalf("router ID = %q", routerID)
	}
	if got := created["name"]; got != claudeRouterName {
		t.Errorf("name = %#v", got)
	}
	if got := created["client_key"]; got != claudeRouterClientKey {
		t.Errorf("client_key = %#v", got)
	}
	if got := created["personal_oauth_enabled"]; got != true {
		t.Errorf("personal_oauth_enabled = %#v", got)
	}
	if got := created["personal_oauth_fallback_enabled"]; got != false {
		t.Errorf("personal_oauth_fallback_enabled = %#v", got)
	}
	if got := stringSlice(created["enabled_models"]); !slices.Equal(got, []string{
		"anthropic/claude-fable-5",
		"anthropic/claude-opus-5",
		"zai-org/GLM-5.3-Flash",
	}) {
		t.Errorf("enabled_models = %q", got)
	}
	if got := stringSlice(created["eval_ids"]); !slices.Equal(got, defaultAgentEvalIDs) {
		t.Errorf("eval_ids = %q", got)
	}
	levels, _ := created["model_thinking_levels"].(map[string]any)
	for _, modelID := range []string{
		"anthropic/claude-fable-5",
		"anthropic/claude-opus-5",
		"zai-org/GLM-5.3-Flash",
	} {
		if got := stringSlice(levels[modelID]); !slices.Equal(got, []string{"high"}) {
			t.Errorf("%s thinking levels = %q, want [high]", modelID, got)
		}
	}
}

func TestEnsureClaudeAgentRouterUsesManagedRouterAfterOptOut(t *testing.T) {
	var created map[string]any
	var putRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
				{
					"id":                    "rtr_default",
					"name":                  "Default",
					"is_default":            true,
					"enabled_models":        []string{"openai/gpt-5.6-sol"},
					"model_thinking_levels": map[string][]string{"openai/gpt-5.6-sol": {"medium"}},
				},
				{
					"id":                              "rtr_personal",
					"name":                            claudeRouterName,
					"client_key":                      claudeRouterClientKey,
					"personal_oauth_enabled":          true,
					"personal_oauth_fallback_enabled": false,
				},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers/model-catalog":
			writeAgentModelCatalog(t, w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/current/routers":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_managed", "name": claudeManagedRouterName})
		case r.Method == http.MethodPut:
			putRequests++
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	t.Setenv("DARI_API_URL", server.URL)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader("n\n\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_managed" {
		t.Fatalf("router ID = %q", routerID)
	}
	if putRequests != 0 {
		t.Fatalf("PUT requests = %d, want no mutation of personal router", putRequests)
	}
	if got := created["client_key"]; got != claudeManagedRouterClientKey {
		t.Errorf("client_key = %#v", got)
	}
	if got := created["personal_oauth_enabled"]; got != false {
		t.Errorf("personal_oauth_enabled = %#v", got)
	}
}

func TestEnsureClaudeAgentRouterAvoidsPersonalRouterWithoutCredentialsOnEOF(t *testing.T) {
	var writeRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
				{"id": "rtr_default", "name": "Default", "is_default": true},
				{
					"id":                              "rtr_claude",
					"name":                            claudeRouterName,
					"client_key":                      claudeRouterClientKey,
					"personal_oauth_enabled":          true,
					"personal_oauth_fallback_enabled": false,
				},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		case r.Method == http.MethodPost || r.Method == http.MethodPut:
			writeRequests++
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader(""),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_default" {
		t.Fatalf("router ID = %q, want default router", routerID)
	}
	if writeRequests != 0 {
		t.Fatalf("router write requests = %d, want 0", writeRequests)
	}
}

func TestEnsureClaudeAgentRouterPrefersCachedManagedRouterOnEOF(t *testing.T) {
	useTestAPIKey(t)
	const scope = "test-org|agent:claude"
	if err := state.SaveAgentRouterID(scope, "rtr_managed_cached"); err != nil {
		t.Fatal(err)
	}

	var writeRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
				{"id": "rtr_default", "name": "Default", "is_default": true},
				{"id": "rtr_managed_other", "client_key": claudeManagedRouterClientKey},
				{"id": "rtr_managed_cached", "client_key": claudeManagedRouterClientKey},
				{"id": "rtr_personal", "client_key": claudeRouterClientKey},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		case r.Method == http.MethodPost || r.Method == http.MethodPut:
			writeRequests++
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader(""),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_managed_cached" {
		t.Fatalf("router ID = %q, want cached managed router", routerID)
	}
	if writeRequests != 0 {
		t.Fatalf("router write requests = %d, want 0", writeRequests)
	}
}

func TestEnsureClaudeAgentRouterIgnoresCachedPersonalRouterOnEOF(t *testing.T) {
	useTestAPIKey(t)
	const scope = "test-org|agent:claude"
	if err := state.SaveAgentRouterID(scope, "rtr_personal"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
				{"id": "rtr_default", "name": "Default", "is_default": true},
				{"id": "rtr_managed", "client_key": claudeManagedRouterClientKey},
				{"id": "rtr_personal", "client_key": claudeRouterClientKey},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader(""),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_managed" {
		t.Fatalf("router ID = %q, want managed router", routerID)
	}
}

func TestEnsureClaudeAgentRouterRequiresSubscriptionAfterRename(t *testing.T) {
	var updated map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
				{"id": "rtr_default", "name": "Default", "is_default": true},
				{
					"id":                              "rtr_claude",
					"name":                            "Renamed by user",
					"client_key":                      claudeRouterClientKey,
					"enabled_models":                  []string{"anthropic/claude-fable-5"},
					"model_providers":                 map[string]string{"anthropic/claude-fable-5": "anthropic"},
					"model_thinking_levels":           map[string][]string{"anthropic/claude-fable-5": {"high"}},
					"personal_oauth_enabled":          false,
					"personal_oauth_fallback_enabled": true,
				},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			writeClaudeSubscriptionCredential(w)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/organizations/current/routers/rtr_claude":
			if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_claude"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader(""),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_claude" {
		t.Fatalf("router ID = %q", routerID)
	}
	if got := updated["personal_oauth_enabled"]; got != true {
		t.Errorf("personal_oauth_enabled = %#v", got)
	}
	if got := updated["personal_oauth_fallback_enabled"]; got != false {
		t.Errorf("personal_oauth_fallback_enabled = %#v", got)
	}
	if got := updated["name"]; got != "Renamed by user" {
		t.Errorf("name = %#v", got)
	}
	if got := stringSlice(updated["enabled_models"]); !slices.Equal(got, []string{"anthropic/claude-fable-5"}) {
		t.Errorf("enabled_models = %q", got)
	}
}

func TestEnsureClaudeAgentRouterSkipsUpdateForCompliantRouter(t *testing.T) {
	var putRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{
				{"id": "rtr_default", "name": "Default", "is_default": true},
				{
					"id":                              "rtr_claude",
					"name":                            "Renamed by user",
					"client_key":                      claudeRouterClientKey,
					"personal_oauth_enabled":          true,
					"personal_oauth_fallback_enabled": false,
				},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			writeClaudeSubscriptionCredential(w)
		case r.Method == http.MethodPut:
			putRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_claude"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	t.Setenv("DARI_API_URL", server.URL)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader(""),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_claude" {
		t.Fatalf("router ID = %q", routerID)
	}
	if putRequests != 0 {
		t.Fatalf("PUT requests = %d, want 0", putRequests)
	}
}

func TestEnsureClaudeAgentRouterRecoversFromConcurrentCreate(t *testing.T) {
	var routerLists int
	var subscriptionUpdated bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			routerLists++
			routers := []map[string]any{{
				"id": "rtr_default", "name": "Default", "is_default": true,
			}}
			if routerLists > 1 {
				routers = append(routers, map[string]any{
					"id":             "rtr_other_computer",
					"name":           "Renamed",
					"client_key":     claudeRouterClientKey,
					"enabled_models": []string{"anthropic/claude-fable-5"},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": routers})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			writeClaudeSubscriptionCredential(w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers/model-catalog":
			writeAgentModelCatalog(t, w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/current/routers":
			http.Error(w, `{"detail":"client key already exists"}`, http.StatusConflict)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/organizations/current/routers/rtr_other_computer":
			var updated map[string]any
			if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
				t.Error(err)
			}
			subscriptionUpdated = updated["personal_oauth_enabled"] == true &&
				updated["personal_oauth_fallback_enabled"] == false
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_other_computer"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader("\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_other_computer" {
		t.Fatalf("router ID = %q", routerID)
	}
	if routerLists != 2 {
		t.Fatalf("router list requests = %d, want 2", routerLists)
	}
	if !subscriptionUpdated {
		t.Fatal("recovered router was not updated to require a personal subscription")
	}
}

func TestEnsureCodexAgentRouterPreservesDefaultOnEnter(t *testing.T) {
	var putRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{{
				"id":             "rtr_default",
				"name":           "Custom Default",
				"is_default":     true,
				"enabled_models": []string{"custom/model"},
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers/model-catalog":
			writeAgentModelCatalog(t, w)
		case r.Method == http.MethodPut:
			putRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_default"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	t.Setenv("DARI_API_URL", server.URL)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"codex",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader("\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_default" {
		t.Fatalf("router ID = %q", routerID)
	}
	if putRequests != 0 {
		t.Fatalf("PUT requests = %d, want 0", putRequests)
	}
}

func writeClaudeSubscriptionCredential(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []map[string]any{{
		"oauth_provider": "anthropic_claude_code",
	}}})
}

func writeAgentModelCatalog(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	_ = json.NewEncoder(w).Encode(map[string]any{"groups": []map[string]any{{
		"provider":             "managed",
		"supports_managed_key": true,
		"models": []map[string]any{
			{"id": "anthropic/claude-fable-5", "display_name": "Fable", "provider": "anthropic", "default_provider": "anthropic", "default_thinking_level": "high", "supports_managed_key": true},
			{"id": "anthropic/claude-opus-5", "display_name": "Opus", "provider": "anthropic", "default_provider": "anthropic", "default_thinking_level": "high", "supports_managed_key": true},
			{"id": "zai-org/GLM-5.3-Flash", "display_name": "GLM Flash", "provider": "fireworks", "default_provider": "fireworks", "default_thinking_level": "high", "supports_managed_key": true},
			{"id": "zai-org/GLM-5.3", "display_name": "GLM", "provider": "fireworks", "default_provider": "fireworks", "default_thinking_level": "high", "supports_managed_key": true},
		},
	}}})
}

func stringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		text, _ := value.(string)
		result = append(result, text)
	}
	return result
}

func testRecommendedLevels() map[string][]string {
	return map[string][]string{
		"anthropic/claude-fable-5": {"high"},
		"zai-org/GLM-5.3":          {"high"},
		"openai/gpt-5.6-sol":       {"medium", "xhigh"},
	}
}

func testAgentModels() []agentModel {
	return []agentModel{
		{
			ID:                   "anthropic/claude-fable-5",
			DisplayName:          "Fable",
			Provider:             "anthropic",
			DefaultProvider:      "anthropic",
			DefaultThinkingLevel: "medium",
		},
		{
			ID:                   "zai-org/GLM-5.3",
			DisplayName:          "GLM",
			Provider:             "fireworks",
			DefaultProvider:      "fireworks",
			DefaultThinkingLevel: "high",
		},
		{
			ID:                   "openai/gpt-5.6-sol",
			DisplayName:          "Sol",
			Provider:             "openai",
			DefaultProvider:      "openai",
			DefaultThinkingLevel: "medium",
		},
	}
}

func TestEnsureCodexAgentRouterSkipsPutForUnchangedSelectionInAnyOrder(t *testing.T) {
	var putRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			// Default router lists the same models as the picker defaults,
			// but in the opposite order.
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{{
				"id":                    "rtr_default",
				"name":                  "Default",
				"is_default":            true,
				"enabled_models":        []string{"zai-org/GLM-5.3", "anthropic/claude-fable-5"},
				"model_providers":       map[string]string{"anthropic/claude-fable-5": "anthropic", "zai-org/GLM-5.3": "fireworks"},
				"model_thinking_levels": map[string][]string{"anthropic/claude-fable-5": {"high"}, "zai-org/GLM-5.3": {"high"}},
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers/model-catalog":
			writeAgentModelCatalog(t, w)
		case r.Method == http.MethodPut:
			putRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_default"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	t.Setenv("DARI_API_URL", server.URL)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"codex",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader("\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_default" {
		t.Fatalf("router ID = %q", routerID)
	}
	if putRequests != 0 {
		t.Fatalf("PUT requests = %d, want 0", putRequests)
	}
}

func TestPickerLevelOnlyChangeProducesUpdate(t *testing.T) {
	// The TUI is the only path that can change thinking levels without
	// changing the model set, so drive its state machine directly.
	models := testAgentModels()
	models[0].SupportedLevels = []string{"low", "high", "max"}
	current := agentRouter{
		EnabledModels:       []string{"anthropic/claude-fable-5"},
		ModelProviders:      map[string]string{"anthropic/claude-fable-5": "anthropic"},
		ModelThinkingLevels: map[string][]string{"anthropic/claude-fable-5": {"high"}},
	}
	p := newAgentPickerState(models, current.EnabledModels, current.ModelThinkingLevels, current.ModelThinkingLevels)

	// Open Fable's levels: cursor on high (index 1). Move to max, check it,
	// move back to high, uncheck it, then leave editing.
	for _, key := range []string{"right", "down", " ", "up", " ", "left", "enter"} {
		if _, canceled := p.handleKey(key); canceled {
			t.Fatal("picker canceled")
		}
	}
	choices := p.choices()
	if len(choices) != 1 || choices[0].ID != "anthropic/claude-fable-5" {
		t.Fatalf("choices = %#v", choices)
	}
	if got := choices[0].Levels; !slices.Equal(got, []string{"max"}) {
		t.Fatalf("fable levels = %q, want [max]", got)
	}
	if !agentRouterNeedsUpdate(current, choices) {
		t.Fatal("level-only edit was ignored")
	}
	body := agentRouterUpdateBody(current, models, choices)
	if got := body.ModelThinkingLevels["anthropic/claude-fable-5"]; !slices.Equal(got, []string{"max"}) {
		t.Fatalf("update body levels = %q, want [max]", got)
	}
}

func TestAgentRouterNeedsUpdateIgnoresModelOrder(t *testing.T) {
	current := agentRouter{
		EnabledModels:       []string{"zai-org/GLM-5.3", "anthropic/claude-fable-5"},
		ModelThinkingLevels: map[string][]string{"anthropic/claude-fable-5": {"high"}, "zai-org/GLM-5.3": {"high"}},
	}
	choices := []agentModelChoice{
		{ID: "anthropic/claude-fable-5", Levels: []string{"high"}},
		{ID: "zai-org/GLM-5.3", Levels: []string{"high"}},
	}
	if agentRouterNeedsUpdate(current, choices) {
		t.Fatal("update triggered for the same models in a different order")
	}
}

func TestAgentRouterNeedsUpdateDetectsLevelOnlyEdit(t *testing.T) {
	current := agentRouter{
		EnabledModels:       []string{"anthropic/claude-fable-5"},
		ModelThinkingLevels: map[string][]string{"anthropic/claude-fable-5": {"high"}},
	}
	choices := []agentModelChoice{
		{ID: "anthropic/claude-fable-5", Levels: []string{"max"}},
	}
	if !agentRouterNeedsUpdate(current, choices) {
		t.Fatal("level-only edit was ignored")
	}
}

func TestRenderAgentPickerAlignsColumnsInRawMode(t *testing.T) {
	models := testAgentModels()
	models[0].DisplayName = "A Very Long Model Name Indeed"
	p := newAgentPickerState(models, []string{"anthropic/claude-fable-5"}, nil, testRecommendedLevels())
	p.handleKey("down")
	var out strings.Builder
	renderAgentPicker(&out, "Configure", p, 0)

	// Raw mode needs \r\n; bare \n staircases every line one column right.
	if strings.Contains(out.String(), "\n") && strings.Contains(strings.ReplaceAll(out.String(), "\r\n", ""), "\n") {
		t.Fatalf("render contains bare \\n newlines:\n%q", out.String())
	}

	// The thinking column must line up across rows regardless of name length
	// or styling; measure the visible column by stripping ANSI escapes.
	thinkCols := map[int]bool{}
	for _, line := range strings.Split(stripANSI(out.String()), "\r\n") {
		if col := strings.Index(line, "thinking:"); col >= 0 {
			thinkCols[col] = true
		}
	}
	if len(thinkCols) != 1 {
		t.Fatalf("thinking column positions = %v, want one aligned column:\n%s", thinkCols, out.String())
	}

	// Redrawing must clear every row to end of line so shorter lines and
	// spacer rows leave no remnants from earlier screen output.
	frameLines := strings.Split(out.String(), "\r\n")
	for _, line := range frameLines {
		if line != "" && !strings.Contains(line, "\033[K") && !strings.HasSuffix(line, "\033[J") {
			t.Fatalf("frame line not cleared to EOL: %q", line)
		}
	}
}

func TestAgentPickerNeverSubmitsEmptyLevels(t *testing.T) {
	// A model with supported levels but no recommended set, no default
	// level, and no current router levels must still get a level.
	models := []agentModel{{
		ID:              "openai/gpt-5.6-terra",
		DisplayName:     "Terra",
		Provider:        "openai",
		DefaultProvider: "openai",
		SupportedLevels: []string{"low", "medium", "high"},
	}}
	p := newAgentPickerState(models, []string{"openai/gpt-5.6-terra"}, nil, nil)
	if got := p.rows[0].checkedLevels(); !slices.Equal(got, []string{"low"}) {
		t.Fatalf("initial levels = %q, want first supported [low]", got)
	}

	// Emptying the level checklist and leaving restores a usable set too.
	p.handleKey("right")
	p.handleKey(" ") // uncheck low
	if got := p.rows[0].checkedLevels(); len(got) != 0 {
		t.Fatalf("levels = %q, want none", got)
	}
	p.handleKey("left")
	if got := p.rows[0].checkedLevels(); !slices.Equal(got, []string{"low"}) {
		t.Fatalf("levels after exit = %q, want fallback [low]", got)
	}

	done, canceled := p.handleKey("enter")
	if !done || canceled {
		t.Fatalf("enter = done %v canceled %v", done, canceled)
	}
	choices := p.choices()
	if len(choices) != 1 || len(choices[0].Levels) == 0 {
		t.Fatalf("choices = %#v, want one model with levels", choices)
	}
}

func stripANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	return re.ReplaceAllString(s, "")
}

func TestRenderAgentPickerCentersBannerOnly(t *testing.T) {
	p := newAgentPickerState(testAgentModels(), nil, nil, testRecommendedLevels())
	var out strings.Builder
	renderAgentPicker(&out, "Configure a router for Claude Code", p, 120)
	lines := strings.Split(stripANSI(out.String()), "\r\n")

	// The banner box is centered on the terminal.
	var border string
	for _, line := range lines {
		if strings.Contains(line, "╭") {
			border = line
			break
		}
	}
	if border == "" {
		t.Fatalf("banner box missing:\n%s", out.String())
	}
	lead := len(border) - len(strings.TrimLeft(border, " "))
	boxWidth := len([]rune(strings.TrimSpace(border)))
	if want := (120 - boxWidth) / 2; lead != want || lead <= 0 {
		t.Fatalf("banner leading pad = %d, want %d: %q", lead, want, border)
	}

	// The checklist and footer stay left-aligned.
	for _, line := range lines {
		if strings.Contains(line, "Fable") || strings.Contains(line, "Defaults are recommended") {
			if lead := len(line) - len(strings.TrimLeft(line, " ")); lead > 2 {
				t.Fatalf("picker line should stay left-aligned (pad %d): %q", lead, line)
			}
		}
	}

	// The title is centered inside the box.
	for _, line := range lines {
		if strings.Contains(line, "Configure a router for Claude Code") {
			first, last := strings.Index(line, "│"), strings.LastIndex(line, "│")
			inner := line[first+len("│") : last]
			leftPad := len(inner) - len(strings.TrimLeft(inner, " "))
			rightPad := len(inner) - len(strings.TrimRight(inner, " "))
			if abs(leftPad-rightPad) > 1 {
				t.Fatalf("title not centered in box: left %d right %d: %q", leftPad, rightPad, line)
			}
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestEnsureClaudeAgentRouterIgnoresMatchingDefaultRouter(t *testing.T) {
	// Even when the org default router is customized to exactly Claude's
	// models with different levels, Claude onboarding must recommend its own
	// set with the catalog's recommended levels.
	var created map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []map[string]any{{
				"id":         "rtr_default",
				"name":       "Default",
				"is_default": true,
				"enabled_models": []string{
					"anthropic/claude-fable-5",
					"anthropic/claude-opus-5",
					"zai-org/GLM-5.3-Flash",
				},
				"model_thinking_levels": map[string][]string{
					"anthropic/claude-fable-5": {"low"},
					"anthropic/claude-opus-5":  {"medium"},
					"zai-org/GLM-5.3-Flash":    {"medium"},
				},
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/credentials":
			writeClaudeSubscriptionCredential(w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/organizations/current/routers/model-catalog":
			writeAgentModelCatalog(t, w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/current/routers":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_claude", "name": claudeRouterName})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	useTestAPIKey(t)
	t.Setenv("DARI_API_URL", server.URL)
	routerID, err := ensureAgentRouter(
		context.Background(),
		"claude",
		agentRoutingAccess{apiURL: server.URL, scope: "test-org", key: "dari_route"},
		strings.NewReader("\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if routerID != "rtr_claude" {
		t.Fatalf("router ID = %q", routerID)
	}
	levels, _ := created["model_thinking_levels"].(map[string]any)
	for modelID, want := range map[string][]string{
		"anthropic/claude-fable-5": {"high"},
		"anthropic/claude-opus-5":  {"high"},
		"zai-org/GLM-5.3-Flash":    {"high"},
	} {
		if got := stringSlice(levels[modelID]); !slices.Equal(got, want) {
			t.Errorf("%s levels = %q, want recommended %q", modelID, got, want)
		}
	}
}
