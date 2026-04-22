package visualtest

import (
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// WPTServerConfig carries the hostnames and ports used to substitute WPT
// "sub" template tokens and to recognise rewritable URLs in iframe/image
// src attributes. Defaults match the canonical upstream wpt-runner config.
type WPTServerConfig struct {
	PrimaryHost string
	AltHost     string
	HTTPPort    int
	HTTPPort1   int
	HTTPSPort   int
}

// DefaultWPTServerConfig returns the canonical upstream WPT server config.
func DefaultWPTServerConfig() WPTServerConfig {
	return WPTServerConfig{
		PrimaryHost: "web-platform.test",
		AltHost:     "not-web-platform.test",
		HTTPPort:    8000,
		HTTPPort1:   8001,
		HTTPSPort:   8443,
	}
}

var wptTokenPattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// ApplyWPTSubstitutions processes WPT sub.html template tokens in the given
// content. testFilePath is the absolute filesystem path of the source file;
// wptRoot is the absolute root of the test tree. location[path] is computed
// as a "/"-rooted URL path relative to wptRoot. Content without "{{" tokens
// is returned unchanged.
func ApplyWPTSubstitutions(content, testFilePath, wptRoot string) string {
	return ApplyWPTSubstitutionsWithConfig(content, testFilePath, wptRoot, DefaultWPTServerConfig())
}

// ApplyWPTSubstitutionsWithConfig is ApplyWPTSubstitutions with an explicit
// server config.
func ApplyWPTSubstitutionsWithConfig(content, testFilePath, wptRoot string, cfg WPTServerConfig) string {
	if !strings.Contains(content, "{{") {
		return content
	}
	locationPath := ""
	if testFilePath != "" && wptRoot != "" {
		if rel, err := filepath.Rel(wptRoot, testFilePath); err == nil {
			locationPath = "/" + filepath.ToSlash(rel)
		}
	}
	return wptTokenPattern.ReplaceAllStringFunc(content, func(match string) string {
		key := match[2 : len(match)-2]
		switch key {
		case "host":
			return cfg.PrimaryHost
		case "hosts[]", "hosts[][]":
			return cfg.PrimaryHost
		case "hosts[alt]", "hosts[alt][]":
			return cfg.AltHost
		case "hosts[][www]":
			return "www." + cfg.PrimaryHost
		case "hosts[alt][www]":
			return "www." + cfg.AltHost
		case "ports[http][0]":
			return strconv.Itoa(cfg.HTTPPort)
		case "ports[http][1]":
			return strconv.Itoa(cfg.HTTPPort1)
		case "ports[https][0]":
			return strconv.Itoa(cfg.HTTPSPort)
		case "location[path]":
			return locationPath
		case "location[host]":
			return cfg.PrimaryHost + ":" + strconv.Itoa(cfg.HTTPPort)
		case "location[server]":
			return "http://" + cfg.PrimaryHost + ":" + strconv.Itoa(cfg.HTTPPort)
		case "location[scheme]":
			return "http"
		}
		return match
	})
}

// stripWPTHost removes a rewritable scheme+host+port prefix from uri,
// returning the path portion (with "/../" segments normalised) and true if
// the URL matched a configured WPT host. URLs that are already plain paths
// or use unrelated hosts return (uri, false).
func stripWPTHost(uri string, cfg WPTServerConfig) (string, bool) {
	orig := uri
	switch {
	case strings.HasPrefix(uri, "http://"):
		uri = uri[len("http://"):]
	case strings.HasPrefix(uri, "https://"):
		uri = uri[len("https://"):]
	case strings.HasPrefix(uri, "//"):
		uri = uri[2:]
	default:
		return orig, false
	}
	slash := strings.IndexByte(uri, '/')
	var hostport, rest string
	if slash < 0 {
		hostport, rest = uri, "/"
	} else {
		hostport, rest = uri[:slash], uri[slash:]
	}
	host := hostport
	if colon := strings.IndexByte(hostport, ':'); colon >= 0 {
		host = hostport[:colon]
	}
	candidates := []string{
		cfg.PrimaryHost,
		"www." + cfg.PrimaryHost,
		cfg.AltHost,
		"www." + cfg.AltHost,
	}
	for _, want := range candidates {
		if host == want {
			return path.Clean(rest), true
		}
	}
	return orig, false
}
