package js

import (
	"fmt"
	"strings"
	"testing"
)

// runEventLog runs script in an empty page and returns the window global
// `log`, an array of strings the script's listeners push, joined by spaces.
func runEventLog(t *testing.T, script string) string {
	t.Helper()
	doc := parseHTML(t, `<!DOCTYPE html><body></body>`)
	eng := New()
	doc.Scripts = append(doc.Scripts, "var log = [];\n"+script)
	if err := eng.Execute(doc); err != nil {
		t.Fatal(err)
	}
	var entries []string
	if err := eng.vm.ExportTo(eng.vm.Get("log"), &entries); err != nil {
		t.Fatal(err)
	}
	return strings.Join(entries, " ")
}

// TestEventTarget_TestRenderedAtRootElement: the reftest harness dispatches
// a bubbling "TestRendered" event at the root element
// (/common/reftest-wait.js's harness side), so a listener on
// document.documentElement runs, then the event bubbles to the document.
// Dispatch follows EventDispatcher::Dispatch: capture listeners from the
// document down, the target, then bubble listeners up
// (core/dom/events/event_dispatcher.cc:294-370 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); listeners are added and
// removed per EventTarget::addEventListener / removeEventListener
// (event_target.cc:528-583, :728-770), a duplicate (same callback and
// capture) is ignored, and an options dictionary's `once` does not make a
// capture listener. TestRendered fires once, so `once` removal itself is not
// observable here; window listeners share the document's table in louis14,
// so the window step of the path is not separately covered.
func TestEventTarget_TestRenderedAtRootElement(t *testing.T) {
	cases := []struct {
		name, script, want string
	}{
		{
			name: "root listener, then bubbling to the document",
			script: `
				document.documentElement.addEventListener("TestRendered", function (e) {
					log.push("root:" + e.eventPhase + ":" + (e.target === document.documentElement) + ":" + (this === document.documentElement));
				});
				document.addEventListener("TestRendered", function (e) {
					log.push("document:" + e.eventPhase + ":" + (e.currentTarget === document));
				});`,
			want: "root:2:true:true document:3:true",
		},
		{
			name: "capture on the document runs first",
			script: `
				document.documentElement.addEventListener("TestRendered", function () { log.push("root"); });
				document.addEventListener("TestRendered", function () { log.push("doc-capture"); }, true);
				document.addEventListener("TestRendered", function () { log.push("doc-bubble"); });`,
			want: "doc-capture root doc-bubble",
		},
		{
			name: "stopPropagation at the root stops bubbling",
			script: `
				document.documentElement.addEventListener("TestRendered", function (e) { log.push("root"); e.stopPropagation(); });
				document.addEventListener("TestRendered", function () { log.push("document"); });`,
			want: "root",
		},
		{
			name: "remove, duplicate, and once is not capture",
			script: `
				function f() { log.push("f"); }
				function g() { log.push("g"); }
				var root = document.documentElement;
				root.addEventListener("TestRendered", f);
				root.addEventListener("TestRendered", f);
				root.addEventListener("TestRendered", g, {once: true});
				root.addEventListener("other", g);
				root.removeEventListener("other", g);
				root.removeEventListener("TestRendered", f, true);`,
			want: "f g",
		},
		{
			name: "a listener on a descendant does not run",
			script: `
				document.body.addEventListener("TestRendered", function () { log.push("body"); });
				document.documentElement.addEventListener("TestRendered", function () { log.push("root"); });`,
			want: "root",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runEventLog(t, tc.script); got != tc.want {
				t.Errorf("log = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEventTarget_ElementLoadReachesListeners: an element's load / error
// event goes to its addEventListener listeners as well as its onload /
// onerror handler, which is one entry in the same listener list, kept in
// its first-set place when replaced and removed when set to null
// (EventTarget::SetAttributeEventListener, core/dom/events/
// event_target.cc:813-827; HTMLFrameOwnerElement::DispatchLoad →
// DispatchScopedEvent, core/html/html_frame_owner_element.cc:606-608;
// ImageLoader::DispatchPendingLoadEvent, core/loader/image_loader.cc:
// 1010-1030 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestEventTarget_ElementLoadReachesListeners(t *testing.T) {
	t.Run("iframe", func(t *testing.T) {
		got := runEventLog(t, `
			var i = document.createElement("iframe");
			i.src = "data:text/html,hi";
			i.addEventListener("load", function (e) { log.push("listener:" + (this === i) + ":" + e.eventPhase); });
			i.onload = function () { log.push("first idl"); };
			i.addEventListener("load", function () { log.push("listener2"); });
			i.onload = function () { log.push("idl"); };
			document.body.appendChild(i);`)
		if want := "listener:true:2 idl listener2"; got != want {
			t.Errorf("log = %q, want %q", got, want)
		}
	})
	t.Run("onload = null removes the handler", func(t *testing.T) {
		got := runEventLog(t, `
			var i = document.createElement("iframe");
			i.onload = function () { log.push("idl"); };
			i.onload = null;
			i.addEventListener("load", function () { log.push("listener"); });
			document.body.appendChild(i);`)
		if want := "listener"; got != want {
			t.Errorf("log = %q, want %q", got, want)
		}
	})
	t.Run("img", func(t *testing.T) {
		got := runImageScript(t, New(), fmt.Sprintf(`
			var img = new Image();
			img.addEventListener("load", function () { log("listener:" + (this === img)); });
			img.onload = function () { log("idl"); };
			img.src = %q;`, pngDataURL(t, 3, 2)))
		if want := "listener:true;idl;"; got != want {
			t.Errorf("log = %q, want %q", got, want)
		}
	})
}
