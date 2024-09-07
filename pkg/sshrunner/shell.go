package sshrunner

import (
	"fmt"
	"strings"
)

// shellQuote properly quotes a string for use in a shell command
func shellQuote(s string) string {
	// Use single quotes if the string contains spaces or special characters
	if strings.ContainsAny(s, " '\"\\$`") {
		return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "'\"'\"'"))
	}
	return s
}
