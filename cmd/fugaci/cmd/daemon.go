package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const fugaciPlistPath = "/Library/LaunchDaemons/io.fugaci.plist"

// Template for the plist file
const fugaciPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        {{- range .Args }}
        <string>{{.}}</string>
        {{- end }}
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/output.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/error.log</string>
</dict>
</plist>
`

// PlistData holds data for populating the template
type PlistData struct {
	Label      string
	BinaryPath string
	Args       []string
	LogDir     string
}

// GeneratePlist generates a .plist file based on the current binary
func GeneratePlist(label, logDir string, args []string, plistPath string) error {
	// Get the current binary path
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Absolute log directory path
	absLogDir, err := filepath.Abs(logDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for log directory: %w", err)
	}

	// Create the plist data struct
	data := PlistData{
		Label:      label,
		BinaryPath: binaryPath,
		Args:       args,
		LogDir:     absLogDir,
	}

	// Parse the template and create the file
	tmpl, err := template.New("plist").Parse(fugaciPlistTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Open or create the plist file
	file, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("failed to create plist file: %w", err)
	}
	defer file.Close()

	// Execute the template and write to the file
	err = tmpl.Execute(file, data)
	if err != nil {
		return fmt.Errorf("failed to write to plist file: %w", err)
	}

	return nil
}

func NewCmdDaemon() *cobra.Command {
	viper.SetDefault("LogLevel", "info")
	var daemonCommand = &cobra.Command{
		Use:       "daemon",
		Short:     "top-level for managing daemon",
		Long:      ``,
		ValidArgs: []string{"init", "start", "stop"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		Run: func(cmd *cobra.Command, args []string) {
		},
	}

	var daemonBootstrapCmd = &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstraps a service into a domain",
		Run: func(cmd *cobra.Command, args []string) {
			plistLogDir := "/var/log/fugaci/"
			plistArgs := []string{"serve"}

			err := GeneratePlist("io.fugaci", plistLogDir, plistArgs, fugaciPlistPath)
			if err != nil {
				fmt.Printf("Error generating plist: %v\n", err)
				return
			}

			fmt.Printf("Plist generated successfully at: %s\n", fugaciPlistPath)

			// Bootstrap the service
			out, err := exec.Command("launchctl", "bootstrap", "system", fugaciPlistPath).CombinedOutput()
			if err != nil {
				fmt.Printf("Error bootstrapping service: %s\n", err)
				fmt.Printf("Output: %s\n", string(out))
			} else {
				fmt.Println("Service bootstrapped successfully")
			}
		},
	}

	var daemonBootoutCmd = &cobra.Command{
		Use:   "bootout",
		Short: "Tears down a service from a domain",
		Run: func(cmd *cobra.Command, args []string) {
			// Bootout the service
			out, err := exec.Command("launchctl", "bootout", "system", fugaciPlistPath).CombinedOutput()
			if err != nil {
				fmt.Printf("Error tearing down service: %s\n", err)
				fmt.Printf("Output: %s\n", string(out))
			} else {
				fmt.Println("Service booted out successfully")
			}
		},
	}

	daemonCommand.AddCommand(
		daemonBootstrapCmd,
		daemonBootoutCmd,
	)
	return daemonCommand
}
