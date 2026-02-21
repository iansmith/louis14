//go:build ignore

package main

import (
	"fmt"
	"os"
	"strings"
)

func patch(content, old, new string) string {
	if !strings.Contains(content, old) {
		fmt.Fprintf(os.Stderr, "PATCH FAILED: pattern not found:\n%q\n", old)
		os.Exit(1)
	}
	return strings.Replace(content, old, new, 1)
}

func main() {
	fname := os.Args[1]
	data, _ := os.ReadFile(fname)
	content := string(data)

	// Patch 1: Add LayerName to Rule struct
	content = patch(content,
		"	ContainerQuery *ContainerQuery   // Optional @container query wrapper\n}",
		"	ContainerQuery *ContainerQuery   // Optional @container query wrapper\n\tLayerName      string            // @layer name (empty = unlayered)\n}",
	)

	// Patch 2: Add LayerOrder to Stylesheet struct
	content = patch(content,
		"type Stylesheet struct {\n\tRules     []Rule\n\tFontFaces []FontFaceRule\n}",
		"type Stylesheet struct {\n\tRules      []Rule\n\tFontFaces  []FontFaceRule\n\tLayerOrder []string      // @layer declaration order (first declared = lowest priority)\n}",
	)

	// Patch 3: In splitRules, emit @layer statement rules
	content = patch(content,
		"		if !isAfterCloseBrace {\n\t\t\t\t// At-rule terminator — skip the semicolon\n\t\t\t\tstart = i + 1\n\t\t\t}",
		"		if !isAfterCloseBrace {\n\t\t\t\t// At-rule terminator — emit rule text if it's an @layer statement\n\t\t\t\truleText := strings.TrimSpace(css[start : i+1])\n\t\t\t\tif strings.HasPrefix(ruleText, \"@layer\") {\n\t\t\t\t\trules = append(rules, ruleText)\n\t\t\t\t}\n\t\t\t\tstart = i + 1\n\t\t\t}",
	)

	// Patch 4: In ParseStylesheet, add @layer handling
	content = patch(content,
		"		} else if strings.HasPrefix(trimmed, \"@container\") {\n\t\t\t\tcontainerRules := parseContainerRule(ruleStr)\n\t\t\t\tstylesheet.Rules = append(stylesheet.Rules, containerRules...)\n\t\t\t}\n\t\t\t// Unknown at-rules (@three-dee, @import, etc.) are silently skipped\n\t\t\tcontinue",
		"		} else if strings.HasPrefix(trimmed, \"@container\") {\n\t\t\t\tcontainerRules := parseContainerRule(ruleStr)\n\t\t\t\tstylesheet.Rules = append(stylesheet.Rules, containerRules...)\n\t\t\t} else if strings.HasPrefix(trimmed, \"@layer\") {\n\t\t\t\tlayerRules, layerNames := parseLayerRule(ruleStr, stylesheet.LayerOrder)\n\t\t\t\tfor _, name := range layerNames {\n\t\t\t\t\tfound := false\n\t\t\t\t\tfor _, existing := range stylesheet.LayerOrder {\n\t\t\t\t\t\tif existing == name {\n\t\t\t\t\t\t\tfound = true\n\t\t\t\t\t\t\tbreak\n\t\t\t\t\t\t}\n\t\t\t\t\t}\n\t\t\t\t\tif !found {\n\t\t\t\t\t\tstylesheet.LayerOrder = append(stylesheet.LayerOrder, name)\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t\tstylesheet.Rules = append(stylesheet.Rules, layerRules...)\n\t\t\t}\n\t\t\t// Unknown at-rules (@three-dee, @import, etc.) are silently skipped\n\t\t\tcontinue",
	)

	if err := os.WriteFile(fname, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}
