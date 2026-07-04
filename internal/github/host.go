package github

import (
	"fmt"
	"strings"
)

// DefaultHost is the GitHub host targeted when none is specified.
const DefaultHost = "github.com"

// NormalizeHost validates a host and returns the canonical form used to
// build API and web URLs. getRelease supports github.com and GitHub
// Enterprise Cloud with data residency (*.ghe.com); self-hosted GitHub
// Enterprise Server is not supported since it uses a different API
// convention (/api/v3 on a customer-owned domain).
func NormalizeHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case host == "", host == DefaultHost, host == "www."+DefaultHost:
		return DefaultHost, nil
	case strings.HasSuffix(host, ".ghe.com"):
		return host, nil
	default:
		return "", fmt.Errorf("unsupported GitHub host %q: getRelease supports github.com or a *.ghe.com host (GitHub Enterprise Cloud with data residency); self-hosted GitHub Enterprise Server is not supported", host)
	}
}
