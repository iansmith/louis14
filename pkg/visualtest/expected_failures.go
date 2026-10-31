package visualtest

import (
	"bufio"
	"os"
	"strings"
)

// LoadExpectedFailures parses an expected-failures manifest into a map of
// test identifier -> reason. A missing file yields an empty map (no expected
// failures). Blank lines and lines beginning with '#' are ignored; the
// identifier and reason are separated by a tab (falling back to the first
// whitespace run).
func LoadExpectedFailures(path string) (map[string]string, error) {
	m := map[string]string{}
	f, err := os.Open(path)
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
