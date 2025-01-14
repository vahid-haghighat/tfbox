package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/vahid-haghighat/tfbox/internal"
	"os"
)

var workingDirectory string
var tfVersion string
var rootDirectory string

var rootCmd = &cobra.Command{
	Use:   "tfbox",
	Short: "Runs terraform inside docker",
	Long:  `"Runs terraform inside docker"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tfArgs := cmd.Flags().Args()

		if len(tfArgs) == 0 {
			return fmt.Errorf("you need to pass terraform commands/flags")
		}
		return internal.Run(rootDirectory, workingDirectory, tfVersion, tfArgs)
	},
	Args: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&rootDirectory, "root", "r", "", "The root of the project")
	rootCmd.Flags().StringVarP(&workingDirectory, "directory", "d", ".", "Terraform working directory relative to root directory")
	rootCmd.Flags().StringVarP(&tfVersion, "version", "v", "", "Terraform version to use")
}
