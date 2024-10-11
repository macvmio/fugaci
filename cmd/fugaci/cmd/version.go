package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version can be set via:
// -ldflags="-X 'github.com/macvmio/fugaci/cmd/fugaci/cmd.Version=$TAG'"
var Version string

func init() {
	if Version == "" {
		i, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		Version = i.Main.Version
	}
}

// NewCmdVersion creates a new cobra.Command for the version subcommand.
func NewCmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Long:  `The version string is completely dependent on how the binary was built,").`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			if Version == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "could not determine build information")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), Version)
			}
		},
	}
}
