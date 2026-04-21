package cli

import (
	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/scaffold"
)

func init() {
	commandRegistrars = append(commandRegistrars, func(root *cobra.Command, gf *globalFlags) {
		root.AddCommand(newInitCmd())
	})
}

func newInitCmd() *cobra.Command {
	var (
		name  string
		skill string
		force bool
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
			result, err := scaffold.Run(scaffold.Options{
				TargetDir: target,
				Name:      name,
				Skill:     skill,
				Force:     force,
			})
			if err != nil {
				return err
			}
			return printJSON(map[string]any{
				"project_root":  result.ProjectRoot,
				"project_name":  result.ProjectName,
				"skill_name":    result.SkillName,
				"written_files": result.WrittenFiles,
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Project name for dari.yml (defaults to the directory name)")
	cmd.Flags().StringVar(&skill, "skill", "review", "Name of the example skill to create under skills/")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files in the target directory")
	return cmd
}
