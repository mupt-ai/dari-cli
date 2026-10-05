package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type agentTUISession struct {
	file     *os.File
	stderr   io.Writer
	oldState *term.State
	width    int
}

func startAgentTUISession(stdin io.Reader, stderr io.Writer) (*agentTUISession, error) {
	file, ok := stdin.(*os.File)
	if !ok {
		return nil, errors.New("interactive onboarding requires a terminal")
	}
	fd := int(file.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("enter interactive mode: %w", err)
	}
	width := 0
	if w, _, err := term.GetSize(fd); err == nil {
		width = w
	}
	fmt.Fprint(stderr, "\033[?25l")
	return &agentTUISession{file: file, stderr: stderr, oldState: oldState, width: width}, nil
}

func (session *agentTUISession) Close() {
	_ = term.Restore(int(session.file.Fd()), session.oldState)
	fmt.Fprint(session.stderr, "\033[?25h")
}

func (session *agentTUISession) render(lines []string) {
	renderAgentScreen(session.stderr, lines, session.width)
}

type agentChoiceOption struct {
	label string
	note  string
}

func runAgentChoiceTUI(
	stdin io.Reader,
	stderr io.Writer,
	title string,
	prompt string,
	options []agentChoiceOption,
	details func(width int) []string,
) (int, error) {
	session, err := startAgentTUISession(stdin, stderr)
	if err != nil {
		return 0, err
	}
	defer session.Close()

	if len(options) == 0 {
		return 0, errors.New("interactive selection has no options")
	}
	cursor := 0
	buf := make([]byte, 16)
	for {
		lines := agentBannerLines(title)
		if details != nil {
			lines = append(lines, details(session.width)...)
		}
		session.render(agentChoiceLines(lines, prompt, options, cursor))
		n, readErr := session.file.Read(buf)
		for _, key := range parseAgentPickerKeys(buf[:n]) {
			switch key {
			case "up", "k":
				cursor = (cursor - 1 + len(options)) % len(options)
			case "down", "j":
				cursor = (cursor + 1) % len(options)
			case "enter":
				return cursor, nil
			case "quit", "esc":
				return 0, errors.New("agent onboarding canceled")
			}
		}
		if readErr != nil {
			return 0, fmt.Errorf("read selection: %w", readErr)
		}
	}
}

func agentChoiceLines(lines []string, prompt string, options []agentChoiceOption, cursor int) []string {
	lines = append(lines, ansiBold+prompt+ansiReset, "")
	for index, option := range options {
		marker := "  "
		labelStyle := ""
		if index == cursor {
			marker = ansiCoral + "> " + ansiReset
			labelStyle = ansiBold
		}
		line := marker + labelStyle + option.label + ansiReset
		if option.note != "" {
			line += "  " + ansiGreen + "· " + option.note + ansiReset
		}
		lines = append(lines, line)
	}
	return append(lines, "", ansiGray+"↑/↓ move   enter confirm   q cancel"+ansiReset)
}

func agentOAuthLines(provider agentSubscriptionProvider, authorizationURL string, opened bool, width int) []string {
	return append(agentBannerLines("Connect "+provider.name), agentOAuthDetails(provider, authorizationURL, opened, width)...)
}

func agentOAuthDetails(provider agentSubscriptionProvider, authorizationURL string, opened bool, width int) []string {
	lines := []string{}
	if opened {
		lines = append(lines, ansiGreen+"✓ "+provider.name+" login opened in your browser."+ansiReset)
	} else {
		lines = append(lines, ansiBold+"Open this URL in a browser:"+ansiReset)
	}
	lineWidth := width - 4
	if lineWidth <= 0 {
		lineWidth = len(authorizationURL)
	}
	for len(authorizationURL) > lineWidth {
		lines = append(lines, ansiCyan+authorizationURL[:lineWidth]+ansiReset)
		authorizationURL = authorizationURL[lineWidth:]
	}
	return append(lines, ansiCyan+authorizationURL+ansiReset, "")
}

func runAgentCodeInputTUI(
	provider agentSubscriptionProvider,
	stdin io.Reader,
	stderr io.Writer,
	authorizationURL string,
	opened bool,
) (string, error) {
	session, err := startAgentTUISession(stdin, stderr)
	if err != nil {
		return "", err
	}
	defer session.Close()

	input := agentCodeInputState{}
	buf := make([]byte, 256)
	for {
		lines := append(agentOAuthLines(provider, authorizationURL, opened, session.width),
			ansiBold+"Paste Authorization Code"+ansiReset,
			ansiGray+"Approve access, then copy the code Anthropic shows."+ansiReset,
			"",
			ansiCoral+"> "+ansiReset+codeInputDisplay(input.value, session.width),
			"",
			ansiGray+"enter submit   backspace edit   ctrl+c cancel"+ansiReset,
		)
		session.render(lines)

		n, readErr := session.file.Read(buf)
		submitted, canceled := input.update(buf[:n])
		if canceled {
			return "", fmt.Errorf("%s authorization canceled", provider.name)
		}
		if submitted && strings.TrimSpace(input.value) != "" {
			return input.value, nil
		}
		if readErr != nil {
			return "", fmt.Errorf("read %s authorization code: %w", provider.name, readErr)
		}
	}
}

type agentCodeInputState struct {
	value  string
	escape bool
	csi    bool
}

func (input *agentCodeInputState) update(raw []byte) (bool, bool) {
	for _, value := range raw {
		if input.escape {
			if input.csi {
				if isTerminalEscapeFinal(value) {
					input.escape = false
					input.csi = false
				}
				continue
			}
			input.escape = false
			if value == '[' {
				input.escape = true
				input.csi = true
				continue
			}
		}
		if value == 0x1b {
			input.escape = true
			continue
		}
		switch value {
		case '\r', '\n':
			return true, false
		case 0x03:
			return false, true
		case 0x08, 0x7f:
			if len(input.value) > 0 {
				input.value = input.value[:len(input.value)-1]
			}
		default:
			if value >= 0x20 && value <= 0x7e {
				input.value += string(value)
			}
		}
	}
	return false, false
}

func isTerminalEscapeFinal(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') || value == '~'
}

func codeInputDisplay(input string, width int) string {
	if input == "" {
		return ansiDim + "code#state" + ansiReset
	}
	available := width - 4
	if available <= 0 || len(input) <= available {
		return ansiCyan + input + ansiReset
	}
	return ansiGray + "…" + ansiReset + ansiCyan + input[len(input)-available+1:] + ansiReset
}
