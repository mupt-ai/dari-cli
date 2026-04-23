package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{Use: "credentials", Short: "Manage runtime credentials for the current org"}
		cmd.AddCommand(
			newCredentialsListCmd(gf),
			newCredentialsAddCmd(gf),
			newCredentialsRemoveCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newCredentialsListCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored credential names for the current org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp struct {
				Credentials []any `json:"credentials"`
			}
			if err := orgJWTRequest(cmd, gf, http.MethodGet, "/credentials", nil, &resp); err != nil {
				return err
			}
			return printJSON(map[string]any{"credentials": resp.Credentials})
		},
	}
}

func newCredentialsAddCmd(gf *globalFlags) *cobra.Command {
	var valueStdin bool
	cmd := &cobra.Command{
		Use:   "add <name> [value]",
		Short: "Create or update a runtime credential for the current org",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			var explicitValue *string
			if len(args) == 2 {
				v := args[1]
				explicitValue = &v
			}
			value, err := resolveCredentialValue(name, explicitValue, valueStdin)
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := orgJWTRequest(cmd, gf, http.MethodPut,
				"/credentials/"+url.PathEscape(name),
				map[string]string{"value": value}, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&valueStdin, "value-stdin", false, "Read the credential value from standard input")
	return cmd
}

func newCredentialsRemoveCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Delete a runtime credential from the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgJWTRequest(cmd, gf, http.MethodDelete,
				"/credentials/"+url.PathEscape(args[0]), nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

// resolveCredentialValue mirrors the Python CLI's logic: positional value,
// --value-stdin, or a secure prompt — exactly one must produce a non-empty
// string.
func resolveCredentialValue(name string, explicit *string, useStdin bool) (string, error) {
	if explicit != nil && useStdin {
		return "", errors.New("pass either VALUE or --value-stdin, not both")
	}
	switch {
	case useStdin:
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		v := strings.TrimRight(string(data), "\r\n")
		if v == "" {
			return "", errors.New("credential value must be non-empty")
		}
		return v, nil
	case explicit != nil:
		fmt.Fprintln(os.Stderr, "Warning: passing credential values on the command line can expose them via shell history and process arguments.")
		if *explicit == "" {
			return "", errors.New("credential value must be non-empty")
		}
		return *explicit, nil
	default:
		fmt.Fprintf(os.Stderr, "%s: ", name)
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			// Fallback for non-TTY stdin (e.g. piped input without --value-stdin).
			line, rerr := bufio.NewReader(os.Stdin).ReadString('\n')
			if rerr != nil && rerr != io.EOF {
				return "", rerr
			}
			raw = []byte(strings.TrimRight(line, "\r\n"))
		}
		if len(raw) == 0 {
			return "", errors.New("credential value must be non-empty")
		}
		return string(raw), nil
	}
}
