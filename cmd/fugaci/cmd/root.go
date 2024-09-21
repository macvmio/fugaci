package cmd

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"os/signal"
)

func InitializeCommands() *cobra.Command {
	cobra.OnInitialize(initConfig)
	var rootCmd = &cobra.Command{
		Use:                        "fugaci",
		Short:                      "Fugaci is a kubelet-like provider.",
		Long:                       `Fugaci is a kubelet-like provider which integrates with macOS VM virtualization tool called Curie`,
		ValidArgs:                  []string{"serve", "daemon", "settings"},
		Args:                       cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		SuggestionsMinimumDistance: 2,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(cmd.Short)
			return nil
		},
	}

	// Define the --verbose global flag
	var verbose bool
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Bind the verbose flag to Viper
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))

	rootCmd.AddCommand(
		NewCmdServe(),
		NewCmdDaemon(),
		NewCmdSettings(),
	)

	return rootCmd
}

func Execute(rootCmd *cobra.Command) {
	rootCmd.Version = Version
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
