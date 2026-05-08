package cli

import (
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/scaffold"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		root.AddCommand(newInitCmd(gf))
	})
}

func newInitCmd(gf *globalFlags) *cobra.Command {
	var (
		name      string
		skill     string
		force     bool
		recursive bool
		orgAPIKey string
	)
	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Scaffold a new Dari agent project in the target directory.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			resolvedOrgAPIKey, err := resolveRecursiveOrgAPIKey(recursive, orgAPIKey)
			if err != nil {
				return err
			}
			apiURL := ""
			if recursive {
				apiURL, err = gf.resolveAPIURL()
				if err != nil {
					return err
				}
			}
			result, err := scaffold.Run(scaffold.Options{
				TargetDir: target,
				Name:      name,
				Skill:     skill,
				Force:     force,
				Recursive: recursive,
				OrgAPIKey: resolvedOrgAPIKey,
				APIURL:    apiURL,
			})
			if err != nil {
				return err
			}
			return printJSON(map[string]any{
				"project_root":  result.ProjectRoot,
				"project_name":  result.ProjectName,
				"skill_name":    result.SkillName,
				"recursive":     result.Recursive,
				"written_files": result.WrittenFiles,
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Project name for dari.yml (defaults to the directory name)")
	cmd.Flags().StringVar(&skill, "skill", "", "Name of the example skill to create under skills/ (default: review, or recursive-delegation with --recursive)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files in the target directory")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "Scaffold an agent that can deploy and start recursive child Dari agents")
	cmd.Flags().StringVar(&orgAPIKey, "org-api-key", "", "Org API key to expose to the recursive agent (defaults to $DARI_API_KEY with --recursive)")
	return cmd
}

func resolveRecursiveOrgAPIKey(recursive bool, explicit string) (string, error) {
	trimmed := strings.TrimSpace(explicit)
	if !recursive {
		if trimmed != "" {
			return "", errors.New("--org-api-key can only be used with --recursive")
		}
		return "", nil
	}
	if trimmed != "" {
		return trimmed, nil
	}
	if envValue := strings.TrimSpace(os.Getenv("DARI_API_KEY")); envValue != "" {
		return envValue, nil
	}
	return "", errors.New("--org-api-key is required with --recursive unless DARI_API_KEY is set")
}
