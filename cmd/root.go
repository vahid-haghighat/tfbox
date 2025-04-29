package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/vahid-haghighat/tfbox/internal"
	"os"
)

var rootCmd = &cobra.Command{
	Use:                "tfbox",
	Short:              "Runs terraform inside docker",
	Long:               `"Runs terraform inside docker"`,
	DisableFlagParsing: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		initialize()
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		tfArgs := parsArgs(args)

		if len(tfArgs) == 0 {
			return fmt.Errorf("you need to pass terraform commands/flags")
		}

		return internal.Run(flags["root"].Variable, flags["directory"].Variable, flags["version"].Variable, tfArgs, true)
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
	for _, flag := range flags {
		rootCmd.Flags().StringVarP(&flag.Variable, flag.Name, flag.Shorthand, flag.Default, flag.Usage)
	}
}
