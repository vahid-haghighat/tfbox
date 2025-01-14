package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// absPath returns the absolute path of a given path
func absPath(targetPath string) (string, error) {
	if strings.Contains(targetPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		targetPath = strings.ReplaceAll(targetPath, "~", home)
	}
	return filepath.Abs(targetPath)
}

// dynamicLogPrint clears and redraws the log section dynamically
func dynamicLogPrint(logBuffer []string, maxLines int) {
	fmt.Print("\033[H\033[J") // Clear only visible output (top-left cursor + clear screen)

	// Print the last N logs dynamically
	for _, log := range logBuffer {
		fmt.Println(log)
	}

	// Fill remaining lines with empty content (to clear old logs, if any)
	for i := len(logBuffer); i < maxLines; i++ {
		fmt.Println() // Print blank lines to overwrite the remaining screen space
	}
}
