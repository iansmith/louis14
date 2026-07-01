package js

import "testing"

// TestCSSHighlightsSetRegistersHighlight mirrors the WPT idiom shared by
// every LOU-354 target test (e.g. highlight-painting-currentcolor-001.html):
//
//	CSS.highlights.set("foo", new Highlight(range));
//
// After running, the engine's document should record the Highlight's
// range(s) under CustomHighlights["foo"], and "foo" should be the sole
// entry in CustomHighlightOrder (first-registration order — see
// html.Document.CustomHighlightOrder's doc comment).
func TestCSSHighlightsSetRegistersHighlight(t *testing.T) {
	doc := parseHTML(t, `<div id="target">Selected Text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var range = document.createRange();
		range.selectNodeContents(document.getElementById("target"));
		CSS.highlights.set("foo", new Highlight(range));
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	ranges, ok := doc.CustomHighlights["foo"]
	if !ok {
		t.Fatal(`doc.CustomHighlights["foo"] missing after CSS.highlights.set`)
	}
	if len(ranges) != 1 {
		t.Fatalf("doc.CustomHighlights[\"foo\"] len = %d, want 1", len(ranges))
	}
	target := getElementById(doc.Root, "target")
	if ranges[0].StartContainer != target {
		t.Errorf("StartContainer = %v, want #target div", ranges[0].StartContainer)
	}

	if len(doc.CustomHighlightOrder) != 1 || doc.CustomHighlightOrder[0] != "foo" {
		t.Errorf("CustomHighlightOrder = %v, want [\"foo\"]", doc.CustomHighlightOrder)
	}
}

// TestCSSHighlightsSetReplacesWithoutMovingOrder mirrors the "a second
// .set() with the same name replaces its ranges without moving its stacking
// position" contract from the LOU-354 ticket: registering "a" then "b" then
// "a" again must leave CustomHighlightOrder as ["a", "b"], not ["b", "a"].
func TestCSSHighlightsSetReplacesWithoutMovingOrder(t *testing.T) {
	doc := parseHTML(t, `<div id="target">Selected Text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var target = document.getElementById("target");
		var r1 = document.createRange();
		r1.setStart(target.firstChild, 0);
		r1.setEnd(target.firstChild, 3);
		CSS.highlights.set("a", new Highlight(r1));

		var r2 = document.createRange();
		r2.setStart(target.firstChild, 4);
		r2.setEnd(target.firstChild, 8);
		CSS.highlights.set("b", new Highlight(r2));

		var r3 = document.createRange();
		r3.setStart(target.firstChild, 9);
		r3.setEnd(target.firstChild, 13);
		CSS.highlights.set("a", new Highlight(r3));
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	if len(doc.CustomHighlightOrder) != 2 || doc.CustomHighlightOrder[0] != "a" || doc.CustomHighlightOrder[1] != "b" {
		t.Fatalf("CustomHighlightOrder = %v, want [\"a\", \"b\"]", doc.CustomHighlightOrder)
	}
	ranges := doc.CustomHighlights["a"]
	if len(ranges) != 1 || ranges[0].StartOffset != 9 || ranges[0].EndOffset != 13 {
		t.Errorf("CustomHighlights[\"a\"] = %+v, want the replaced r3 range (9,13)", ranges)
	}
}

// TestHighlightConstructorAcceptsMultipleRanges covers `new Highlight(r1,
// r2, ...)` — the DOM spec's Highlight constructor is variadic
// (https://drafts.csswg.org/css-highlight-api-1/#dom-highlight-highlight).
// No LOU-354 target test actually passes more than one Range (confirmed via
// grep — every target calls `new Highlight(range)` with exactly one
// argument), but this guards the constructor doesn't silently drop
// additional arguments if a future test needs them.
func TestHighlightConstructorAcceptsMultipleRanges(t *testing.T) {
	doc := parseHTML(t, `<div id="target">Selected Text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var target = document.getElementById("target");
		var r1 = document.createRange();
		r1.setStart(target.firstChild, 0);
		r1.setEnd(target.firstChild, 3);
		var r2 = document.createRange();
		r2.setStart(target.firstChild, 4);
		r2.setEnd(target.firstChild, 8);
		CSS.highlights.set("multi", new Highlight(r1, r2));
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	ranges := doc.CustomHighlights["multi"]
	if len(ranges) != 2 {
		t.Fatalf("CustomHighlights[\"multi\"] len = %d, want 2", len(ranges))
	}
}

// TestHighlightPriorityDefaultsToZero covers the Highlight#priority field
// (LOU-354 checkpoint 1: "exposes a settable .priority field defaulting to
// 0 for future-proofing even though no target test uses it") — mirrors
// Blink's Highlight::priority() default (third_party/blink/renderer/core/
// highlight/highlight.h @ blob
// f8a065b0a122c72251cb3a9c056b024583941369).
func TestHighlightPriorityDefaultsToZero(t *testing.T) {
	doc := parseHTML(t, `<div id="target">text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var range = document.createRange();
		range.selectNodeContents(document.getElementById("target"));
		var h = new Highlight(range);
		if (h.priority !== 0) throw new Error("default priority = " + h.priority + ", want 0");
		h.priority = 5;
		if (h.priority !== 5) throw new Error("priority after set = " + h.priority + ", want 5");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestExecCommandSelectAllSelectsWholeDocument covers
// document.execCommand("selectAll") (selection-over-highlight-001.html and
// highlight-painting-005-crash.html): must select all of the document
// body's contents, equivalent to selectNodeContents(document.body) —
// mirrors Blink's SelectAll editing command
// (third_party/blink/renderer/core/editing/commands/select_all_command.cc),
// simplified to louis14's single-Range Selection model (see
// html.Document.Selection's doc comment).
func TestExecCommandSelectAllSelectsWholeDocument(t *testing.T) {
	doc := parseHTML(t, `<body><div id="content">hello</div></body>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.execCommand("selectAll");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	sel := engine.CurrentSelection()
	if sel == nil {
		t.Fatal("CurrentSelection() returned nil after execCommand(selectAll)")
	}
	body := findBodyNode(doc.Root)
	if sel.StartContainer != body {
		t.Errorf("StartContainer = %v, want <body>", sel.StartContainer)
	}
	if sel.StartOffset != 0 {
		t.Errorf("StartOffset = %d, want 0", sel.StartOffset)
	}
	if sel.EndContainer != body || sel.EndOffset != len(body.Children) {
		t.Errorf("end = (%v, %d), want (<body>, %d)", sel.EndContainer, sel.EndOffset, len(body.Children))
	}
}
