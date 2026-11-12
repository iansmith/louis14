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

// A rule's style setter drops an invalid display and keeps the previous
// value (see TestStyleSetInvalidDisplayKeepsPreviousValue).
func TestRuleStyleSetInvalidDisplayKeepsPreviousValue(t *testing.T) {
	doc := parseHTML(t, `<style>div { display: flex; }</style><div>x</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.styleSheets[0].cssRules[0].style.display = "ruby-base";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if got := css.ParseDocumentStylesheets(doc)[0].Rules[0].Declarations["display"]; got != "flex" {
		t.Errorf("display = %q, want %q", got, "flex")
	}
}

// TestStyleSheetsRuleEditsReachTheCascade: a CSSOM edit of a rule's
// declarations reaches the cascade, so an added declaration applies and a
// removed one stops applying (CSSStyleRule::style mutates the rule's
// property set, core/css/css_style_rule.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestStyleSheetsRuleEditsReachTheCascade(t *testing.T) {
	doc := parseHTML(t, `<style>div{color:cyan} div{background-color:red;color:blue}</style><div id="d">x</div>`)
	css.ParseDocumentStylesheets(doc)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.styleSheets[0].cssRules[0].style.backgroundColor = "magenta";
		document.styleSheets[0].cssRules[1].style.color = "";
		delete document.styleSheets[0].cssRules[1].style.backgroundColor;
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	st := css.ApplyStylesToDocument(doc, 800, 600)[getElementById(doc.Root, "d")]
	if got, _ := st.Get("background-color"); got != "magenta" {
		t.Errorf("background-color = %q, want magenta (added to rule 0, removed from rule 1)", got)
	}
	if got, _ := st.Get("color"); got != "cyan" {
		t.Errorf("color = %q, want cyan (removed from rule 1)", got)
	}
}

// TestStyleSheetsRuleEditsNormalizeLikeTheParser: a CSSOM write to a rule
// stores what the stylesheet parser would for the same declaration — a
// logical longhand under its storage key, a shorthand as its longhands,
// comments stripped — so it cascades like a parsed declaration at its new
// place, after the rule's earlier declarations
// (MutableCSSPropertyValueSet::ParseAndSetProperty @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestStyleSheetsRuleEditsNormalizeLikeTheParser(t *testing.T) {
	for _, tc := range []struct{ name, html, script, prop, want string }{
		{"logical after physical", `<style>#d{margin-left:3px}</style><div id="d">x</div>`, `r[0].style.marginInlineStart="9px";`, "margin-left", "9px"},
		{"logical in a later rule", `<style>#d{margin-left:3px} #d{color:red}</style><div id="d">x</div>`, `r[1].style.marginInlineStart="9px";`, "margin-left", "9px"},
		{"margin shorthand after logical", `<style>#d{margin-inline-start:3px}</style><div id="d">x</div>`, `r[0].style.margin="9px";`, "margin-left", "9px"},
		{"margin-inline after physical", `<style>#d{margin-left:3px}</style><div id="d">x</div>`, `r[0].style.marginInline="9px";`, "margin-left", "9px"},
		{"inline-size after width", `<style>#d{width:3px}</style><div id="d">x</div>`, `r[0].style.inlineSize="9px";`, "width", "9px"},
		{"border shorthand after logical colour", `<style>#d{border-inline-start-color:red}</style><div id="d">x</div>`, `r[0].style.border="1px solid lime";`, "border-left-color", "lime"},
		{"comment stripped", `<style>#d{width:3px}</style><div id="d">x</div>`, `r[0].style.width="5px /* !important */";`, "width", "5px"},
		{"logical read back", `<style>#d{color:red}</style><div id="d">x</div>`, `r[0].style.marginInlineStart="9px"; if (r[0].style.marginInlineStart !== "9px") throw new Error("read back " + r[0].style.marginInlineStart);`, "margin-left", "9px"},
		{"logical removed", `<style>#d{margin-inline-start:3px}</style><div id="d">x</div>`, `r[0].style.marginInlineStart="";`, "margin-left", ""},
	} {
		doc := parseHTML(t, tc.html)
		css.ParseDocumentStylesheets(doc)
		engine := New()
		doc.Scripts = append(doc.Scripts, `var r = document.styleSheets[0].cssRules; `+tc.script)
		if err := engine.Execute(doc); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		st := css.ApplyStylesToDocument(doc, 800, 600)[getElementById(doc.Root, "d")]
		if got, _ := st.Get(tc.prop); got != tc.want {
			t.Errorf("%s: %s = %q, want %q", tc.name, tc.prop, got, tc.want)
		}
	}
}

// TestStyleSheetsRuleWritesParseAsOneValue: a CSSOM write to a rule must
// parse as exactly the named property's value, or nothing changes, so a `;`
// cannot inject another declaration (CSSParser::ParseValue,
// core/css/parser/css_parser.cc:179-230 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A shorthand reads back
// serialized from its longhands (StylePropertySerializer::SerializeShorthand,
// core/css/style_property_serializer.cc:591).
func TestStyleSheetsRuleWritesParseAsOneValue(t *testing.T) {
	for _, tc := range []struct{ name, html, script, prop, want string }{
		{"injected declaration in a value", `<style>#d{width:3px}</style><div id="d">x</div>`, `r[0].style.color="lime;width:9px";`, "width", "3px"},
		{"injected declaration in a custom property", `<style>#d{width:3px}</style><div id="d">x</div>`, `r[0].style["--x"]="1px;width:9px";`, "width", "3px"},
		{"margin reads back", `<style>#d{color:red}</style><div id="d">x</div>`, `r[0].style.margin="7px"; if (r[0].style.margin !== "7px") throw new Error("margin read back " + JSON.stringify(r[0].style.margin));`, "margin-left", "7px"},
		{"margin reads back four values", `<style>#d{color:red}</style><div id="d">x</div>`, `r[0].style.margin="1px 2px"; if (r[0].style.margin !== "1px 2px") throw new Error("margin read back " + JSON.stringify(r[0].style.margin));`, "margin-left", "2px"},
		{"font reads back", `<style>#d{color:red}</style><div id="d">x</div>`, `r[0].style.font="bold 12px serif"; if (r[0].style.font !== "bold 12px serif") throw new Error("font read back " + JSON.stringify(r[0].style.font));`, "font-size", "12px"},
		{"border reads back", `<style>#d{color:red}</style><div id="d">x</div>`, `r[0].style.border="1px solid lime"; if (r[0].style.border !== "1px solid lime") throw new Error("border read back " + JSON.stringify(r[0].style.border));`, "border-left-color", "lime"},
		{"partial shorthand reads back empty", `<style>#d{margin-top:1px}</style><div id="d">x</div>`, `if (r[0].style.margin !== "") throw new Error("margin read back " + JSON.stringify(r[0].style.margin));`, "margin-top", "1px"},
	} {
		doc := parseHTML(t, tc.html)
		css.ParseDocumentStylesheets(doc)
		engine := New()
		doc.Scripts = append(doc.Scripts, `var r = document.styleSheets[0].cssRules; `+tc.script)
		if err := engine.Execute(doc); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		st := css.ApplyStylesToDocument(doc, 800, 600)[getElementById(doc.Root, "d")]
		if got, _ := st.Get(tc.prop); got != tc.want {
			t.Errorf("%s: %s = %q, want %q", tc.name, tc.prop, got, tc.want)
		}
	}
}
