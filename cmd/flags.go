package cmd

import (
	"strings"
)

type Flag struct {
	Name      string
	Shorthand string
	Default   string
	Usage     string
	Variable  string
}

var flags = map[string]*Flag{
	"root": {
		Name:      "root",
		Shorthand: "r",
		Default:   ".",
		Usage:     "The root of the project",
	},
	"directory": {
		Name:      "directory",
		Shorthand: "d",
		Default:   ".",
		Usage:     "Terraform working directory relative to root directory",
	},
	"version": {
		Name:      "version",
		Shorthand: "v",
		Default:   "",
		Usage:     "Terraform version to use",
	},
}
var shorthandToNameMap map[string]string

func initialize() {
	shorthandToNameMap = make(map[string]string)
	for k, v := range flags {
		shorthandToNameMap[v.Shorthand] = k
	}
}

func parsArgs(args []string) []string {
	var tfArgs []string

	index := 0
	for index < len(args) {
		arg := args[index]
		oneDash := strings.Replace(arg, "-", "", 1)
		twoDashes := strings.Replace(arg, "--", "", 1)

		if _, exists := flags[twoDashes]; exists {
			flags[twoDashes].Variable = args[index+1]
			index++
		} else if _, exists = shorthandToNameMap[oneDash]; exists {
			flags[shorthandToNameMap[oneDash]].Variable = args[index+1]
			index++
		} else {
			tfArgs = append(tfArgs, arg)
		}
		index++
	}

	return tfArgs
}

func countLeadingDashes(arg string) int {
	leadingDashes := 0
	for _, char := range arg {
		if char == '-' {
			leadingDashes++
		} else {
			break
		}
	}
	return leadingDashes
}
