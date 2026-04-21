// Package cli wires up the cobra command tree for the `dari` binary.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/state"
)

// Execute runs the root command. version is injected at build time via
// -ldflags -X main.version=...; an empty string is displayed as "dev".
func Execute(version string) int {
	root := newRootCmd(version)
	if err := root.Execute(); err != nil {
		// Cobra already prints the error — just return a non-zero exit.
		return 1
	}
	return 0
}

// globalFlags hold cross-command options resolved at root level.
type globalFlags struct {
	apiURL string
}

func newRootCmd(version string) *cobra.Command {
	gf := &globalFlags{}
	if version == "" {
		version = "dev"
	}
	cmd := &cobra.Command{
		Use:           "dari",
		Short:         "dari validates, packages, and publishes agent projects to Agent Host.",
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       version,
	}
	cmd.SetVersionTemplate("dari {{.Version}}\n")

	cmd.PersistentFlags().StringVar(&gf.apiURL, "api-url", "", "Override the Dari API base URL (defaults to $DARI_API_URL or the cached value)")
	_ = cmd.PersistentFlags().MarkHidden("api-url")

	// Subcommands are registered by their respective files via init().
	registerCommands(cmd, gf)
	return cmd
}

// commandRegistrars is the set of functions that append subcommands to the
// root. Each command file appends to this slice from its init().
var commandRegistrars []func(*cobra.Command, *globalFlags)

func registerCommands(root *cobra.Command, gf *globalFlags) {
	for _, fn := range commandRegistrars {
		fn(root, gf)
	}
}

// resolveAPIURL implements the flag → env → state → default precedence the
// Python CLI uses. Mirrors management.resolve_api_url.
func (gf *globalFlags) resolveAPIURL() (string, error) {
	if v := strings.TrimSpace(gf.apiURL); v != "" {
		return api.NormalizeURL(v), nil
	}
	if v := strings.TrimSpace(os.Getenv("DARI_API_URL")); v != "" {
		return api.NormalizeURL(v), nil
	}
	s, err := state.Load()
	if err != nil {
		return "", err
	}
	if s.APIURL != "" {
		return api.NormalizeURL(s.APIURL), nil
	}
	return api.DefaultAPIURL, nil
}

// printJSON writes a pretty-printed JSON document to stdout, matching the
// Python CLI's `json.dumps(..., indent=2, sort_keys=True)` layout closely
// enough that consumers parsing stdout don't break.
func printJSON(v any) error {
	enc := newIndentEncoder(os.Stdout)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
