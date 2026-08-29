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
			newProviderCredentialsCmd(gf),
		)
		root.AddCommand(cmd)
	})
}

func newProviderCredentialsCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage saved provider credentials for routers and custom models",
	}
	cmd.AddCommand(
		newProviderCredentialAddCmd(gf),
		newProviderCredentialUpdateCmd(gf),
		newProviderCredentialRemoveCmd(gf),
	)
	return cmd
}

type providerCredentialSecretFlags struct {
	valueStdin      bool
	awsRegion       string
	accessKeyIDEnv  string
	secretKeyEnv    string
	sessionTokenEnv string
}

func (flags *providerCredentialSecretFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flags.valueStdin, "value-stdin", false, "Read an API key from standard input")
	cmd.Flags().StringVar(&flags.awsRegion, "aws-region", "", "AWS region for Bedrock SigV4 authentication")
	cmd.Flags().StringVar(&flags.accessKeyIDEnv, "aws-access-key-id-env", "", "Environment variable containing the AWS access key ID")
	cmd.Flags().StringVar(&flags.secretKeyEnv, "aws-secret-access-key-env", "", "Environment variable containing the AWS secret access key")
	cmd.Flags().StringVar(&flags.sessionTokenEnv, "aws-session-token-env", "", "Optional environment variable containing an AWS session token")
}

func (flags *providerCredentialSecretFlags) auth(label string, explicitValue *string) (map[string]string, error) {
	usingAWS := strings.TrimSpace(flags.awsRegion) != "" ||
		strings.TrimSpace(flags.accessKeyIDEnv) != "" ||
		strings.TrimSpace(flags.secretKeyEnv) != "" ||
		strings.TrimSpace(flags.sessionTokenEnv) != ""
	if !usingAWS {
		value, err := resolveCredentialValue(label, explicitValue, flags.valueStdin)
		if err != nil {
			return nil, err
		}
		return map[string]string{"type": "api_key", "api_key": value}, nil
	}
	if explicitValue != nil || flags.valueStdin {
		return nil, errors.New("API key VALUE and --value-stdin cannot be combined with AWS IAM flags")
	}
	region := strings.TrimSpace(flags.awsRegion)
	if region == "" {
		return nil, errors.New("--aws-region is required for AWS IAM credentials")
	}
	accessKeyID, err := credentialEnvValue("--aws-access-key-id-env", flags.accessKeyIDEnv)
	if err != nil {
		return nil, err
	}
	secretKey, err := credentialEnvValue("--aws-secret-access-key-env", flags.secretKeyEnv)
	if err != nil {
		return nil, err
	}
	auth := map[string]string{
		"type":              "aws_sigv4",
		"region":            region,
		"access_key_id":     accessKeyID,
		"secret_access_key": secretKey,
	}
	if strings.TrimSpace(flags.sessionTokenEnv) != "" {
		sessionToken, err := credentialEnvValue("--aws-session-token-env", flags.sessionTokenEnv)
		if err != nil {
			return nil, err
		}
		auth["session_token"] = sessionToken
	}
	return auth, nil
}

func credentialEnvValue(flagName, envName string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return "", fmt.Errorf("%s is required for AWS IAM credentials", flagName)
	}
	value := os.Getenv(envName)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s references unset or empty environment variable %s", flagName, envName)
	}
	return value, nil
}

func newProviderCredentialAddCmd(gf *globalFlags) *cobra.Command {
	flags := &providerCredentialSecretFlags{}
	cmd := &cobra.Command{
		Use:   "add <provider> <label> [api_key]",
		Short: "Save an API key or AWS IAM credential",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var explicitValue *string
			if len(args) == 3 {
				explicitValue = &args[2]
			}
			auth, err := flags.auth(args[1], explicitValue)
			if err != nil {
				return err
			}
			body := map[string]any{
				"provider": strings.ToLower(strings.TrimSpace(args[0])),
				"label":    strings.TrimSpace(args[1]),
				"auth":     auth,
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPost,
				"/v1/organizations/current/credentials/provider-credentials", body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	flags.register(cmd)
	return cmd
}

func newProviderCredentialUpdateCmd(gf *globalFlags) *cobra.Command {
	flags := &providerCredentialSecretFlags{}
	cmd := &cobra.Command{
		Use:   "update <credential_id> <label> [api_key]",
		Short: "Update a saved provider credential while preserving its ID",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var explicitValue *string
			if len(args) == 3 {
				explicitValue = &args[2]
			}
			body := map[string]any{
				"label": strings.TrimSpace(args[1]),
			}
			if region, ok := flags.regionOnlyUpdate(explicitValue); ok {
				body["region"] = region
			} else {
				auth, err := flags.auth(args[1], explicitValue)
				if err != nil {
					return err
				}
				body["auth"] = auth
			}
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodPut,
				"/v1/organizations/current/credentials/provider-credentials/"+url.PathEscape(args[0]), body, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	flags.register(cmd)
	return cmd
}

func (flags *providerCredentialSecretFlags) regionOnlyUpdate(explicitValue *string) (string, bool) {
	region := strings.TrimSpace(flags.awsRegion)
	return region, region != "" && explicitValue == nil && !flags.valueStdin &&
		strings.TrimSpace(flags.accessKeyIDEnv) == "" &&
		strings.TrimSpace(flags.secretKeyEnv) == "" &&
		strings.TrimSpace(flags.sessionTokenEnv) == ""
}

func newProviderCredentialRemoveCmd(gf *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <credential_id>",
		Short: "Delete an unreferenced saved provider credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]any
			if err := orgKeyRequest(cmd, gf, http.MethodDelete,
				"/v1/organizations/current/credentials/provider-credentials/"+url.PathEscape(args[0]), nil, &resp); err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
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
			if err := orgKeyRequest(cmd, gf, http.MethodGet, "/v1/organizations/current/credentials", nil, &resp); err != nil {
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
			if err := orgKeyRequest(cmd, gf, http.MethodPut,
				"/v1/organizations/current/credentials/"+url.PathEscape(name),
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
			if err := orgKeyRequest(cmd, gf, http.MethodDelete,
				"/v1/organizations/current/credentials/"+url.PathEscape(args[0]), nil, &resp); err != nil {
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
