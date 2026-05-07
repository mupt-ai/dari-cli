package cli

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		cmd := &cobra.Command{Use: "storage", Short: "Connect customer storage for the current org"}
		cmd.AddCommand(newStorageConnectCmd(gf))
		root.AddCommand(cmd)
	})
}

func newStorageConnectCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "connect", Short: "Connect a storage provider"}
	cmd.AddCommand(newStorageConnectGCSCmd(gf))
	return cmd
}

func newStorageConnectGCSCmd(gf *globalFlags) *cobra.Command {
	var bucket string
	var basePrefix string
	var serviceAccountKeyPath string
	cmd := &cobra.Command{
		Use:   "gcs <name>",
		Short: "Create a named GCS storage binding for the current org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(strings.ToLower(args[0]))
			if name == "" {
				return fmt.Errorf("storage binding name must be non-empty")
			}
			if strings.TrimSpace(bucket) == "" {
				return fmt.Errorf("--bucket is required")
			}
			if strings.TrimSpace(basePrefix) == "" {
				return fmt.Errorf("--base-prefix is required")
			}
			if strings.TrimSpace(serviceAccountKeyPath) == "" {
				return fmt.Errorf("--service-account-key is required")
			}

			keyBytes, err := os.ReadFile(serviceAccountKeyPath)
			if err != nil {
				return fmt.Errorf("read service account key: %w", err)
			}
			credentialName := storageCredentialName(name)
			var credentialResp map[string]any
			if err := orgJWTRequest(cmd, gf, http.MethodPut,
				"/credentials/"+credentialName,
				map[string]string{"value": string(keyBytes)}, &credentialResp); err != nil {
				return err
			}

			bindingResp, err := findStorageBindingByName(cmd, gf, name)
			if err != nil {
				return err
			}
			if bindingResp == nil {
				bindingResp = map[string]any{}
				if err := orgJWTRequest(cmd, gf, http.MethodPost, "/storage-bindings",
					map[string]string{
						"provider":                            "gcs",
						"name":                                name,
						"bucket":                              strings.TrimSpace(bucket),
						"base_prefix":                         strings.TrimSpace(basePrefix),
						"service_account_key_credential_name": credentialName,
					}, &bindingResp); err != nil {
					return err
				}
			}

			return printJSON(map[string]any{
				"storage_binding": bindingResp,
				"manifest": map[string]any{
					"sandbox": map[string]string{
						"storage_binding": name,
					},
				},
			})
		},
	}
	cmd.Flags().StringVar(&bucket, "bucket", "", "GCS bucket name")
	cmd.Flags().StringVar(&basePrefix, "base-prefix", "", "GCS prefix under which Dari creates sessions/<session_id>/ folders")
	cmd.Flags().StringVar(&serviceAccountKeyPath, "service-account-key", "", "Path to a GCP service account JSON key")
	return cmd
}

func findStorageBindingByName(cmd *cobra.Command, gf *globalFlags, name string) (map[string]any, error) {
	var listResp struct {
		StorageBindings []map[string]any `json:"storage_bindings"`
	}
	if err := orgJWTRequest(cmd, gf, http.MethodGet, "/storage-bindings", nil, &listResp); err != nil {
		return nil, err
	}
	for _, binding := range listResp.StorageBindings {
		if bindingName, ok := binding["name"].(string); ok && bindingName == name {
			return binding, nil
		}
	}
	return nil, nil
}

func storageCredentialName(name string) string {
	replacer := strings.NewReplacer("-", "_")
	return "GCS_STORAGE_" + strings.ToUpper(replacer.Replace(name)) + "_KEY"
}
