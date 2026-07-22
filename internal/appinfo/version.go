package appinfo

import "strings"

// Version is replaced at build time for release and container builds.
var Version = "dev"

// Current returns the version shown in the user interface.
func Current() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		return "dev"
	}
	if len(version) == 40 && isHex(version) {
		return version[:7]
	}
	return version
}

func isHex(value string) bool {
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}
