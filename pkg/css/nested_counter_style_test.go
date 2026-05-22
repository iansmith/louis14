package css

import "testing"

// LOU-154: @counter-style nested inside @media must be dispatched and
// registered on the stylesheet, carrying the enclosing media query so
// the viewport-time filter can drop non-matching rules.
//
// Before LOU-154 the inner rule fell through to parseRules() and was
// silently discarded; the only reason WPT tests rendered "correctly"
// was the LOU-144 fix returning [0] for undeclared counters combined
// with formatCounterValue not yet consulting the registry.

func TestParseStylesheet_CounterStyleNestedInMedia(t *testing.T) {
	src := `
		@media all {
			@counter-style upper-bracket {
				system: cyclic;
				symbols: "[" "]";
				suffix: " ";
			}
		}
	`
	stylesheet, err := ParseStylesheet(src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stylesheet.CounterStyles) != 1 {
		t.Fatalf("expected 1 counter-style, got %d", len(stylesheet.CounterStyles))
	}
	cs := stylesheet.CounterStyles[0]
	if cs.Name != "upper-bracket" {
		t.Errorf("name: got %q, want %q", cs.Name, "upper-bracket")
	}
	if cs.System != "cyclic" {
		t.Errorf("system: got %q, want %q", cs.System, "cyclic")
	}
	if cs.MediaQuery == nil {
		t.Fatalf("MediaQuery: got nil, want non-nil (enclosing @media all)")
	}
	if cs.MediaQuery.MediaType != "all" {
		t.Errorf("MediaQuery.MediaType: got %q, want %q", cs.MediaQuery.MediaType, "all")
	}
}

func TestParseStylesheet_CounterStyleNestedInSupportsTrue(t *testing.T) {
	// @supports is parse-time evaluated; when the condition passes the
	// nested @counter-style lands in CounterStyles with MediaQuery=nil.
	src := `
		@supports (color: red) {
			@counter-style supports-ok {
				system: cyclic;
				symbols: "x";
				suffix: " ";
			}
		}
	`
	stylesheet, err := ParseStylesheet(src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stylesheet.CounterStyles) != 1 {
		t.Fatalf("expected 1 counter-style, got %d", len(stylesheet.CounterStyles))
	}
	cs := stylesheet.CounterStyles[0]
	if cs.Name != "supports-ok" {
		t.Errorf("name: got %q, want %q", cs.Name, "supports-ok")
	}
	if cs.MediaQuery != nil {
		t.Errorf("MediaQuery: got %+v, want nil (supports already evaluated true)", cs.MediaQuery)
	}
}

func TestParseStylesheet_CounterStyleNestedInSupportsFalse(t *testing.T) {
	// isSupportedPropertyValue only validates the value for a small set
	// of properties (display, position); for everything else it just
	// checks the property name. So we pick a property the engine doesn't
	// know about — Louis14 doesn't support obsolete -no-such-property —
	// to make the @supports condition fail at parse time.
	src := `
		@supports (-no-such-property: 1) {
			@counter-style should-be-dropped {
				system: cyclic;
				symbols: "x";
				suffix: " ";
			}
		}
	`
	stylesheet, err := ParseStylesheet(src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stylesheet.CounterStyles) != 0 {
		t.Fatalf("expected 0 counter-styles (failing @supports), got %d: %+v",
			len(stylesheet.CounterStyles), stylesheet.CounterStyles)
	}
}

func TestParseStylesheet_CounterStyleNestedInSupportsWithMedia(t *testing.T) {
	// @supports passing, then nested @media — counter-style carries the
	// inner media query.
	src := `
		@supports (color: red) {
			@media (min-width: 100px) {
				@counter-style mq-inside-supports {
					system: cyclic;
					symbols: "y";
					suffix: " ";
				}
			}
		}
	`
	stylesheet, err := ParseStylesheet(src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stylesheet.CounterStyles) != 1 {
		t.Fatalf("expected 1 counter-style, got %d", len(stylesheet.CounterStyles))
	}
	cs := stylesheet.CounterStyles[0]
	if cs.MediaQuery == nil {
		t.Fatalf("MediaQuery: got nil, want non-nil (enclosing @media)")
	}
	if len(cs.MediaQuery.Conditions) != 1 || cs.MediaQuery.Conditions[0].Feature != "min-width" {
		t.Errorf("MediaQuery.Conditions: got %+v, want one min-width entry", cs.MediaQuery.Conditions)
	}
}

func TestFilterCounterStylesByMedia_ViewportTimeMatch(t *testing.T) {
	src := `
		@media (min-width: 500px) {
			@counter-style wide-only {
				system: cyclic;
				symbols: "w";
				suffix: " ";
			}
		}
		@counter-style always {
			system: cyclic;
			symbols: "a";
			suffix: " ";
		}
	`
	stylesheet, err := ParseStylesheet(src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Viewport too narrow: only the unconditional rule matches.
	narrow := FilterCounterStylesByMedia([]*Stylesheet{stylesheet}, 400, 800)
	if len(narrow) != 1 || narrow[0].Name != "always" {
		t.Errorf("narrow viewport: got %+v, want [always]", names(narrow))
	}

	// Viewport wide enough: both rules match.
	wide := FilterCounterStylesByMedia([]*Stylesheet{stylesheet}, 800, 800)
	if len(wide) != 2 {
		t.Fatalf("wide viewport: got %d, want 2 — %+v", len(wide), names(wide))
	}
}

func names(rules []CounterStyleRule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.Name
	}
	return out
}
