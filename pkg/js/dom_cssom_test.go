package js

import (
	"testing"

	"louis14/pkg/css"
)

// TestStyleSheetsRuleMutationLandsInParsedCache is the core LOU-351 contract:
// `document.styleSheets[0].cssRules[0].style.backgroundColor = "magenta"` must
// mutate the SAME parsed *css.Stylesheet the layout/paint pipeline already
// holds via doc.ParsedStylesheetsCache (populated by the pre-JS layout pass in
// pkg/visualtest/helpers.go), so the harness's post-JS relayout re-cascades
// from the mutated rule. Mirrors Blink's storage model where CSSOM wrappers
// mutate the StyleRule's property set inside StyleSheetContents
// (core/css/css_style_rule.cc:64-70 @ 4a32dc92bd262649caef90a6e34d351c24008eee).
func TestStyleSheetsRuleMutationLandsInParsedCache(t *testing.T) {
	doc := parseHTML(t, `<style>div { background-color: cyan; }</style><div>Example</div>`)
	// Simulate the pre-JS layout pass: parse and cache the stylesheets.
	before := css.ParseDocumentStylesheets(doc)
	if len(before) != 1 || len(before[0].Rules) != 1 {
		t.Fatalf("precondition: got %d sheets, want 1 with 1 rule", len(before))
	}

	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.styleSheets[0].cssRules[0].style.backgroundColor = "magenta";
		window.__readBack = document.styleSheets[0].cssRules[0].style.backgroundColor;
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	after := css.ParseDocumentStylesheets(doc)
	if after[0] != before[0] {
		t.Fatal("parsed stylesheet cache was rebuilt; CSSOM mutation must land in the cached parse")
	}
	if got := after[0].Rules[0].Declarations["background-color"]; got != "magenta" {
		t.Errorf("background-color = %q, want %q", got, "magenta")
	}
	if got := engine.vm.Get("__readBack").String(); got != "magenta" {
		t.Errorf("style.backgroundColor readback = %q, want %q", got, "magenta")
	}
}

// TestStyleSheetsTargetTextDynamicShape exercises the exact CSSOM surface of
// target-text-dynamic-001.html: a bare ::target-text rule reached through the
// `if (document.styleSheets[0].cssRules[0])` truthiness guard.
func TestStyleSheetsTargetTextDynamicShape(t *testing.T) {
	doc := parseHTML(t, `<style>::target-text { background-color: cyan; }</style><div>Example</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		if (document.styleSheets[0].cssRules[0])
			document.styleSheets[0].cssRules[0].style.backgroundColor = "magenta";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	sheets := css.ParseDocumentStylesheets(doc)
	if got := sheets[0].Rules[0].Declarations["background-color"]; got != "magenta" {
		t.Errorf("::target-text background-color = %q, want %q", got, "magenta")
	}
}

// TestStyleSheetListAndRuleListSurface covers the list plumbing the CSSOM spec
// requires around the indexed access the target tests use: `length` and
// `item(i)` on both StyleSheetList (core/css/style_sheet_list.idl @ 4a32dc92)
// and CSSRuleList (core/css/css_rule_list.idl @ 4a32dc92), with item(i)
// returning the same object as the indexed getter ([SameObject] semantics).
func TestStyleSheetListAndRuleListSurface(t *testing.T) {
	doc := parseHTML(t, `<style>span::target-text { background-color: cyan; } span { background-color: magenta; }</style><span>x</span>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var sheets = document.styleSheets;
		if (sheets.length !== 1) throw new Error("styleSheets.length = " + sheets.length);
		if (sheets.item(0) !== sheets[0]) throw new Error("item(0) !== [0]");
		if (sheets.item(1) !== null) throw new Error("item(1) out of range must be null");
		var rules = sheets[0].cssRules;
		if (rules.length !== 2) throw new Error("cssRules.length = " + rules.length);
		if (rules.item(0) !== rules[0]) throw new Error("rules.item(0) !== rules[0]");
		if (rules.item(2) !== null) throw new Error("rules.item(2) out of range must be null");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestRuleStyleSetOverridesImportantAndEmptyStringRemoves ports the CSSOM
// attribute-setter semantics from Blink's
// AbstractPropertySetCSSStyleDeclaration::SetPropertyInternal
// (core/css/abstract_property_set_css_style_declaration.cc:254 @ 4a32dc92):
// the camelCase setter always writes important=false (clearing a stale
// !important flag), and assigning "" removes the declaration — the same
// behavior the existing inline styleAccessor implements.
func TestRuleStyleSetOverridesImportantAndEmptyStringRemoves(t *testing.T) {
	doc := parseHTML(t, `<style>div { color: red !important; }</style><div>x</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.styleSheets[0].cssRules[0].style.color = "blue";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	rule := &css.ParseDocumentStylesheets(doc)[0].Rules[0]
	if got := rule.Declarations["color"]; got != "blue" {
		t.Errorf("color = %q, want %q", got, "blue")
	}
	if rule.Important["color"] {
		t.Error("overwriting via the attribute setter must clear the !important flag")
	}

	engine2 := New()
	doc.Scripts = append(doc.Scripts[:0], `
		document.styleSheets[0].cssRules[0].style.color = "";
	`)
	if err := engine2.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := rule.Declarations["color"]; ok {
		t.Error("assigning empty string must remove the declaration")
	}
}
