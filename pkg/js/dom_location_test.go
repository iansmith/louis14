package js

import "testing"

// TestLocationHashGetSet covers window.location.hash being both readable
// and writable, including the round-trip of a pre-encoded `:~:text=`
// fragment directive string (LOU-349's target-text tests assign exactly
// this form, e.g. `window.location.hash = "#:~:text=match%20me"`, and the
// 14 target tests never read .hash back — but the WHATWG Location
// interface's `hash` IDL attribute is a plain getter/setter pair, so this
// asserts the round-trip independent of any one test's usage).
func TestLocationHashGetSet(t *testing.T) {
	doc := parseHTML(t, `<p>match me</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		window.location.hash = "#:~:text=match%20me";
		window.__readBack = window.location.hash;
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	got := engine.vm.Get("__readBack").String()
	want := "#:~:text=match%20me"
	if got != want {
		t.Errorf("location.hash readback = %q, want %q", got, want)
	}
}

// TestLocationHrefFragmentOnlyGetSet covers window.location.href being
// settable to a fragment-only string (`#:~:text=...`) — confirmed via grep
// that none of the 14 LOU-349 target tests assign a full absolute URL, only
// a bare fragment, so general URL parsing/resolution is out of scope (see
// dom_location.go's file doc comment).
func TestLocationHrefFragmentOnlyGetSet(t *testing.T) {
	doc := parseHTML(t, `<p>Example</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		location.href = "#:~:text=Example";
		window.__readBack = location.hash;
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	got := engine.vm.Get("__readBack").String()
	want := "#:~:text=Example"
	if got != want {
		t.Errorf("location.hash after href assignment = %q, want %q", got, want)
	}
}

// TestLocationHashAssignmentTriggersFragmentDirectiveHook asserts that
// assigning EITHER location.hash or location.href synchronously invokes the
// fragment-directive matching hook (Checkpoint 2/3's consumer) when the new
// fragment contains a `:~:text=` directive. None of the 14 target tests
// depend on async/event-loop timing (Engine.Execute already runs scripts,
// rAF callbacks, and the TestRendered dispatch all synchronously — see
// engine.go's Execute), so a direct synchronous call into the hook on
// assignment is correct and sufficient.
func TestLocationHashAssignmentTriggersFragmentDirectiveHook(t *testing.T) {
	doc := parseHTML(t, `<p>match me</p>`)
	engine := New()
	var gotFragments []string
	engine.OnFragmentDirective = func(directive string) {
		gotFragments = append(gotFragments, directive)
	}
	doc.Scripts = append(doc.Scripts, `
		window.location.hash = "#:~:text=match%20me";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if len(gotFragments) != 1 {
		t.Fatalf("OnFragmentDirective called %d times, want 1 (got %v)", len(gotFragments), gotFragments)
	}
	if want := "text=match%20me"; gotFragments[0] != want {
		t.Errorf("directive passed to hook = %q, want %q", gotFragments[0], want)
	}
}

// TestLocationHashAssignmentWithoutDirectiveDoesNotFireHook asserts a plain
// (non-`:~:`) hash assignment does NOT invoke the fragment-directive hook —
// only a `:~:` marker present in the new fragment should trigger matching.
func TestLocationHashAssignmentWithoutDirectiveDoesNotFireHook(t *testing.T) {
	doc := parseHTML(t, `<p id="x">hi</p>`)
	engine := New()
	fired := false
	engine.OnFragmentDirective = func(directive string) {
		fired = true
	}
	doc.Scripts = append(doc.Scripts, `
		window.location.hash = "#x";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if fired {
		t.Error("OnFragmentDirective fired for a plain (non-:~:) hash assignment")
	}
}
