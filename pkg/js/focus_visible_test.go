package js

import "testing"

// TestFocusVisibleMatchesScriptFocus pins :focus-visible as Blink's
// SelectorChecker::MatchesFocusVisiblePseudoClass computes it
// (core/css/selector_checker.cc:3826-3866 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a focused element matches unless
// focus({focusVisible: false}) forced it invisible; otherwise it matches when
// the last focus was not from the mouse, which script focus never is.
func TestFocusVisibleMatchesScriptFocus(t *testing.T) {
	doc := parseHTML(t, `<body><div id="a" tabindex="0">a</div><div id="b" tabindex="0">b</div></body>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var a = document.getElementById("a"), b = document.getElementById("b");
		if (a.matches(":focus-visible")) throw new Error("unfocused element matches :focus-visible");
		a.focus();
		if (!a.matches(":focus-visible")) throw new Error("script-focused element does not match :focus-visible");
		b.focus({focusVisible: false});
		if (b.matches(":focus-visible")) throw new Error("focus({focusVisible: false}) still matches :focus-visible");
		if (!b.matches(":focus")) throw new Error("focus({focusVisible: false}) does not match :focus");
		a.focus();
		if (!a.matches(":focus-visible")) throw new Error("refocusing without the option does not match :focus-visible");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
