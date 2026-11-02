package buildinfo

import (
	"fmt"
	"regexp"
	"strings"
)

var semanticVersionPattern = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
)

// CheckCompatibility requires semantic versions from the same major/minor
// release. Patch, prerelease, and build metadata do not affect compatibility.
func CheckCompatibility(clientVersion, serverVersion string) error {
	clientParts, err := compatibilityParts(clientVersion)
	if err != nil {
		return fmt.Errorf("clientVersion: %w", err)
	}
	serverParts, err := compatibilityParts(serverVersion)
	if err != nil {
		return fmt.Errorf("serverVersion: %w", err)
	}
	if clientParts[1] != serverParts[1] || clientParts[2] != serverParts[2] {
		return fmt.Errorf(
			"client version %s is incompatible with server version %s; use a %s.%s.x client",
			clientVersion, serverVersion, serverParts[1], serverParts[2],
		)
	}
	return nil
}

func compatibilityParts(version string) ([]string, error) {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	parts := semanticVersionPattern.FindStringSubmatch(version)
	if parts == nil {
		return nil, fmt.Errorf("version %q is missing or malformed; a semantic version is required", version)
	}
	for _, identifier := range strings.Split(parts[4], ".") {
		if len(identifier) < 2 || identifier[0] != '0' {
			continue
		}
		isNumeric := true
		for _, character := range identifier {
			if character < '0' || character > '9' {
				isNumeric = false
				break
			}
		}
		if isNumeric {
			return nil, fmt.Errorf("version %q has a malformed numeric prerelease identifier", version)
		}
	}
	return parts, nil
}
