package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

var defaultSettings = []string{
	"sudo defaults write /Library/Preferences/SystemConfiguration/com.apple.InternetSharing.default.plist bootpd -dict DHCPLeaseTimeSecs -int 600",
}

func NewCmdSettings() *cobra.Command {
	var settingsCmd = &cobra.Command{
		Use:   "settings",
		Short: "recommended settings for hosts",
		Long:  ``,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Recommended settings:")
			for _, setting := range defaultSettings {
				fmt.Println(setting)
			}
		},
	}
	return settingsCmd
}
