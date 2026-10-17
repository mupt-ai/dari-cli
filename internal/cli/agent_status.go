package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

// routingStatus is the common display contract. Remaining counts turns after
// the current response; the display includes the current turn, as before.
type routingStatus struct {
	Model     string `json:"selected_model"`
	Thinking  string `json:"reasoning_effort"`
	Remaining *int   `json:"lease_turns_remaining"`
}

func (s routingStatus) String() string {
	model := statusLabel(s.Model)
	if model == "" {
		return ""
	}
	parts := []string{model}
	if thinking := statusLabel(s.Thinking); thinking != "" && thinking != "off" {
		parts = append(parts, thinking)
	}
	if s.Remaining != nil && *s.Remaining >= 0 && *s.Remaining < 1_000_000 {
		turns := *s.Remaining + 1
		unit := "turns"
		if turns == 1 {
			unit = "turn"
		}
		parts = append(parts, fmt.Sprintf("lease %d %s left", turns, unit))
	}
	return strings.Join(parts, " · ")
}

func statusLabel(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, value)
	return strings.TrimSpace(value)
}

func routingStatusFromHeaders(headers http.Header) routingStatus {
	status := routingStatus{
		Model:    headers.Get("x-dari-selected-model"),
		Thinking: headers.Get("x-dari-thinking-level"),
	}
	if remaining, err := strconv.Atoi(headers.Get("x-dari-lease-turns-remaining")); err == nil {
		status.Remaining = &remaining
	}
	return status
}

func claudeRoutingStatus(input io.Reader) (routingStatus, error) {
	var payload struct {
		TranscriptPath string `json:"transcript_path"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&payload); err != nil {
		return routingStatus{}, err
	}
	file, err := os.Open(payload.TranscriptPath)
	if err != nil {
		return routingStatus{}, err
	}
	defer file.Close()
	var last routingStatus
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Routing routingStatus `json:"dari_routing"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Type == "assistant" && entry.Message.Routing.Model != "" {
			last = entry.Message.Routing
		}
	}
	return last, scanner.Err()
}

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, _ *globalFlags) {
		var claude bool
		var headersJSON string
		command := &cobra.Command{
			Use:    "agent-status",
			Short:  "Render coding-agent routing status",
			Hidden: true,
			Args:   cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				var status routingStatus
				if claude {
					// Status hooks must stay quiet before the transcript exists or while it
					// contains an incomplete line. Errors must not replace the agent's UI.
					status, _ = claudeRoutingStatus(cmd.InOrStdin())
				} else {
					var values map[string]string
					if err := json.Unmarshal([]byte(headersJSON), &values); err != nil {
						return err
					}
					headers := make(http.Header, len(values))
					for name, value := range values {
						headers.Set(name, value)
					}
					status = routingStatusFromHeaders(headers)
				}
				if text := status.String(); text != "" {
					fmt.Fprintln(cmd.OutOrStdout(), text)
				}
				return nil
			},
		}
		command.Flags().BoolVar(&claude, "claude", false, "Read Claude Code status input from stdin")
		command.Flags().StringVar(&headersJSON, "headers", "{}", "Routing response headers as JSON")
		root.AddCommand(command)
	})
}

func agentStatusExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate Dari status renderer: %w", err)
	}
	return path, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
