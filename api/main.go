package api

import "github.com/vahid-haghighat/tfbox/internal"

func Run(rootDirectory, workingDirectory, tfVersion string, tfArgs []string, showLogs bool) error {
	return internal.Run(rootDirectory, workingDirectory, tfVersion, tfArgs, showLogs)
}
