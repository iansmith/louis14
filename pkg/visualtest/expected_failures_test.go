package visualtest

import (
	"bufio"
	"os"
	"strings"
)

// expectedFailuresPath is the manifest of WPT reftests that louis14 is expected
// to FAIL because Blink — louis14's canonical reference engine (CLAUDE.md
// "Blink is canonical") — also fails them while Gecko passes. Such tests encode
// behavior our reference engine does not implement, so matching Blink means
// failing the WPT reference. The reftest runner reports these as XFAIL instead
// of FAIL, and ALERTS (fails the suite) if one starts passing. See the file
// header in expected_failures.txt for the full policy and wpt.fyi provenance.
//
// Path is relative to the package directory (go test's working directory).
const expectedFailuresPath = "expected_failures.txt"

// loadExpectedFailures parses the manifest into a map of testdata-root-relative
// test path -> reason. A missing file yields an empty map (no expected
// failures). Blank lines and lines beginning with '#' are ignored; the path and
// reason are separated by a tab (falling back to the first whitespace run).
func loadExpectedFailures() (map[string]string, error) {
	m := map[string]string{}
	f, err := os.Open(expectedFailuresPath)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		path, reason, found := strings.Cut(line, "\t")
		if !found {
			if i := strings.IndexAny(line, " \t"); i >= 0 {
				path, reason = line[:i], line[i+1:]
			} else {
				path = line
			}
		}
		path = strings.TrimSpace(path)
		reason = strings.TrimSpace(reason)
		if path != "" {
			m[path] = reason
		}
	}
	return m, sc.Err()
}
