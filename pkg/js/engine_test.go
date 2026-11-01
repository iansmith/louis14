package js

import (
	"os"
	"slices"
	"testing"
	"time"

	"louis14/pkg/html"
	"louis14/pkg/layout"
)

func parseHTML(t *testing.T, s string) *html.Document {
	t.Helper()
	doc, err := html.Parse(s)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return doc
}

func TestGetElementById(t *testing.T) {
	doc := parseHTML(t, `<div id="foo">hello</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("foo");
		if (el === null) throw new Error("element not found");
		if (el.id !== "foo") throw new Error("wrong id: " + el.id);
		if (el.tagName !== "DIV") throw new Error("wrong tagName: " + el.tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestGetElementByIdNotFound(t *testing.T) {
	doc := parseHTML(t, `<div>hello</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("nonexistent");
		if (el !== null) throw new Error("expected null, got: " + el);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestGetElementsByTagName(t *testing.T) {
	doc := parseHTML(t, `<p>one</p><p>two</p><div>three</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var ps = document.getElementsByTagName("p");
		if (ps.length !== 2) throw new Error("expected 2 p tags, got: " + ps.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestGetElementsByClassName(t *testing.T) {
	doc := parseHTML(t, `<div class="a b">one</div><div class="a">two</div><div class="c">three</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var els = document.getElementsByClassName("a");
		if (els.length !== 2) throw new Error("expected 2 elements with class a, got: " + els.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestSetTextContent(t *testing.T) {
	doc := parseHTML(t, `<p id="target">original</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.getElementById("target").textContent = "changed";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	node := getElementById(doc.Root, "target")
	if node == nil {
		t.Fatal("target not found")
	}
	got := getTextContent(node)
	if got != "changed" {
		t.Errorf("textContent = %q, want %q", got, "changed")
	}
}

func TestSetStyleColor(t *testing.T) {
	doc := parseHTML(t, `<p id="target" style="color: red;">text</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.getElementById("target").style.color = "blue";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	node := getElementById(doc.Root, "target")
	style := node.Attributes["style"]
	if !containsDecl(style, "color", "blue") {
		t.Errorf("style = %q, want color: blue", style)
	}
}

func TestSetStyleDisplay(t *testing.T) {
	doc := parseHTML(t, `<p id="target">visible</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.getElementById("target").style.display = "none";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	node := getElementById(doc.Root, "target")
	style := node.Attributes["style"]
	if !containsDecl(style, "display", "none") {
		t.Errorf("style = %q, want display: none", style)
	}
}

func TestSetStyleCamelCase(t *testing.T) {
	doc := parseHTML(t, `<div id="box">box</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("box");
		el.style.backgroundColor = "yellow";
		el.style.fontSize = "20px";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	node := getElementById(doc.Root, "box")
	style := node.Attributes["style"]
	if !containsDecl(style, "background-color", "yellow") {
		t.Errorf("style = %q, want background-color: yellow", style)
	}
	if !containsDecl(style, "font-size", "20px") {
		t.Errorf("style = %q, want font-size: 20px", style)
	}
}

func TestSetAttribute(t *testing.T) {
	doc := parseHTML(t, `<div id="target">text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("target");
		el.setAttribute("data-value", "42");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	node := getElementById(doc.Root, "target")
	if val, ok := node.Attributes["data-value"]; !ok || val != "42" {
		t.Errorf("data-value = %q, want %q", val, "42")
	}
}

func TestGetAttribute(t *testing.T) {
	doc := parseHTML(t, `<div id="target" data-x="hello">text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("target");
		var val = el.getAttribute("data-x");
		if (val !== "hello") throw new Error("getAttribute returned: " + val);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestSetClassName(t *testing.T) {
	doc := parseHTML(t, `<div id="target" class="old">text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.getElementById("target").className = "new-class";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	node := getElementById(doc.Root, "target")
	if node.Attributes["class"] != "new-class" {
		t.Errorf("class = %q, want %q", node.Attributes["class"], "new-class")
	}
}

func TestChildren(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span>a</span><span>b</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var kids = document.getElementById("parent").children;
		if (kids.length !== 2) throw new Error("expected 2 children, got: " + kids.length);
		if (kids[0].tagName !== "SPAN") throw new Error("expected SPAN, got: " + kids[0].tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForAtLeastOneFrameResolvesAndRunsMutation(t *testing.T) {
	// Mirrors the WPT pattern from
	// svg-empty-container-with-filter-content-added.html: wait for one frame,
	// then mutate the DOM. Because rAF runs synchronously and goja drains the
	// promise job queue on RunString return, the .then callback must execute
	// before Execute() returns — verified by reading the mutated DOM after.
	doc := parseHTML(t, `<div id="target"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		waitForAtLeastOneFrame().then(function() {
			var rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
			rect.setAttribute('fill', 'green');
			document.getElementById('target').appendChild(rect);
			takeScreenshot();
		});
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	target := getElementById(doc.Root, "target")
	if target == nil {
		t.Fatal("target not found")
	}
	if len(target.Children) != 1 {
		t.Fatalf("expected 1 child appended via rAF promise, got %d", len(target.Children))
	}
	if target.Children[0].TagName != "rect" {
		t.Errorf("expected appended child tag=rect, got %q", target.Children[0].TagName)
	}
	if target.Children[0].Attributes["fill"] != "green" {
		t.Errorf("expected fill=green, got %q", target.Children[0].Attributes["fill"])
	}
}

func TestWaitForCompositorReadyResolvesAndRunsMutation(t *testing.T) {
	// Mirrors the WPT pattern from
	// css-backgrounds/color-mix-currentcolor-background-repaint.html: await
	// waitForCompositorReady() (defined upstream in
	// /web-animations/testcommon.js, which the harness doesn't fetch), then
	// mutate the DOM — including the load-listener wrapper the real tests
	// use. The prelude must define the symbol as a resolving promise so the
	// mutation runs before Execute() returns.
	doc := parseHTML(t, `<div id="target"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		addEventListener("load", async function() {
			await waitForCompositorReady();
			document.getElementById('target').classList.add('green');
		});
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	target := getElementById(doc.Root, "target")
	if target == nil {
		t.Fatal("target not found")
	}
	if got := target.Attributes["class"]; got != "green" {
		t.Errorf("expected class mutation after awaiting waitForCompositorReady, got class=%q", got)
	}
}

// LOU-432: document.timeline and the animation clock. Each
// requestAnimationFrame frame advances html.Document.AnimationClock by one
// html.ApproximateFrameTime before its callback runs, mirroring Blink's
// AnimationClock ticked per frame (kApproximateFrameTime,
// core/animation/animation_clock.cc:40) and the frame time passed to rAF
// callbacks (ScriptedAnimationController::ExecuteFrameCallbacks,
// core/dom/scripted_animation_controller.cc:176-181), both @
// d4ecdfed88f962439247c2ad36b8fe47805b1520.

func TestDocumentTimelineAdvancesOneFramePerAnimationFrame(t *testing.T) {
	doc := parseHTML(t, `<div id="target"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var rec = [document.timeline.currentTime];
		setTimeout(function() { rec.push(document.timeline.currentTime); });
		requestAnimationFrame(function(ts) {
			rec.push(ts, document.timeline.currentTime);
			requestAnimationFrame(function(ts2) {
				rec.push(ts2, document.timeline.currentTime);
			});
		});
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	f := float64(html.ApproximateFrameTime) / float64(time.Millisecond)
	want := []float64{0, 0, f, f, 2 * f, 2 * f}
	var got []float64
	if err := engine.vm.ExportTo(engine.vm.Get("rec"), &got); err != nil {
		t.Fatalf("export rec: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("rec = %v, want %v", got, want)
	}
	if got, want := doc.AnimationClock.CurrentTime(), 2*html.ApproximateFrameTime; got != want {
		t.Errorf("clock after Execute = %v, want %v", got, want)
	}
}

func TestVendoredWaitForAnimationFramesResolvesAndRunsMutation(t *testing.T) {
	// Runs the upstream WPT helper byte-for-byte (the copy vendored for
	// css-animations/display-none-to-display-block.html). Its
	// waitForAnimationFrames loops on requestAnimationFrame until
	// document.timeline.currentTime has moved and frameCount frames ran;
	// with a frozen timeline it recurses until the call-stack cap below.
	common, err := os.ReadFile("../visualtest/testdata/wpt-css3/css-animations/support/testcommon.js")
	if err != nil {
		t.Fatal(err)
	}
	doc := parseHTML(t, `<div id="target"></div>`)
	engine := New()
	engine.vm.SetMaxCallStackSize(2000)
	doc.Scripts = append(doc.Scripts, string(common), `
		var n = 0;
		onload = async () => {
			const target = document.getElementById('target');
			await waitForAnimationFrames(1);
			target.classList.add('a');
			await waitForAnimationFrames(2, () => n++);
			target.classList.add('b');
		};
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	target := getElementById(doc.Root, "target")
	if got := target.Attributes["class"]; got != "a b" {
		t.Errorf("class = %q, want \"a b\"", got)
	}
	if got := engine.vm.Get("n").ToInteger(); got != 2 {
		t.Errorf("onFrame ran %d times, want 2", got)
	}
	if got, want := doc.AnimationClock.CurrentTime(), 3*html.ApproximateFrameTime; got != want {
		t.Errorf("clock after Execute = %v, want %v", got, want)
	}
}

func TestScriptError(t *testing.T) {
	doc := parseHTML(t, `<p>text</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `throw new Error("test error");`)
	err := engine.Execute(doc)
	if err == nil {
		t.Fatal("expected error from script")
	}
}

func TestScriptExtraction(t *testing.T) {
	doc := parseHTML(t, `<p>text</p><script>var x = 1;</script><script>var y = 2;</script>`)
	if len(doc.Scripts) != 2 {
		t.Fatalf("expected 2 scripts, got %d", len(doc.Scripts))
	}
	if doc.Scripts[0] != "var x = 1;" {
		t.Errorf("script 0 = %q", doc.Scripts[0])
	}
	if doc.Scripts[1] != "var y = 2;" {
		t.Errorf("script 1 = %q", doc.Scripts[1])
	}
}

func TestCamelToKebab(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"color", "color"},
		{"backgroundColor", "background-color"},
		{"fontSize", "font-size"},
		{"fontWeight", "font-weight"},
		{"textDecoration", "text-decoration"},
		{"borderTopWidth", "border-top-width"},
		{"cssFloat", "float"},
	}
	for _, tt := range tests {
		got := camelToKebab(tt.input)
		if got != tt.want {
			t.Errorf("camelToKebab(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseSerializeInlineStyle(t *testing.T) {
	input := "color: red; font-size: 16px"
	m := parseInlineStyle(input)
	if m["color"] != "red" {
		t.Errorf("color = %q, want red", m["color"])
	}
	if m["font-size"] != "16px" {
		t.Errorf("font-size = %q, want 16px", m["font-size"])
	}

	// Modify and serialize
	m["color"] = "blue"
	s := serializeInlineStyle(m)
	m2 := parseInlineStyle(s)
	if m2["color"] != "blue" {
		t.Errorf("after serialize, color = %q, want blue", m2["color"])
	}
	if m2["font-size"] != "16px" {
		t.Errorf("after serialize, font-size = %q, want 16px", m2["font-size"])
	}
}

// containsDecl checks if an inline style string contains a particular property:value.
func containsDecl(style, prop, val string) bool {
	m := parseInlineStyle(style)
	return m[prop] == val
}

// TestElementScrollOffsetSetters pins Element.scrollLeft / scrollTop as
// writers of the element's scroll offset (Element::setScrollLeft /
// setScrollTop, dom/element.cc:2811-2925 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): each setter replaces one axis
// and keeps the other, and the getters read it back. Clamping is layout's
// job (see html.Node.ScrollOffset).
func TestElementScrollOffsetSetters(t *testing.T) {
	doc := parseHTML(t, `<div id="s" style="overflow:hidden"></div>`)
	doc.Scripts = append(doc.Scripts, `
		var s = document.getElementById("s");
		s.scrollTop = 100;
		s.scrollLeft = 30;
		s.scrollTop = 120.5;
		if (s.scrollLeft !== 30) throw new Error("scrollLeft = " + s.scrollLeft);
		if (s.scrollTop !== 120.5) throw new Error("scrollTop = " + s.scrollTop);
	`)
	if err := New().Execute(doc); err != nil {
		t.Fatal(err)
	}
	s := getElementById(doc.Root, "s")
	if s.ScrollOffset != (html.ScrollOffset{X: 30, Y: 120.5}) {
		t.Errorf("ScrollOffset = %+v, want {30 120.5}", s.ScrollOffset)
	}
}

// TestElementScrollOffsetClampedByLayout pins that the scroll bindings
// bring layout up to date first (Document::UpdateStyleAndLayoutForNode in
// Element::scrollTop / setScrollTop, dom/element.cc:2724-2925 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the setter keeps the offset
// layout clamped the request to, the getter reads it, and a box-less
// element neither scrolls nor reports an offset.
func TestElementScrollOffsetClampedByLayout(t *testing.T) {
	doc := parseHTML(t, `<div id="s"></div><div id="n"></div>`)
	s := getElementById(doc.Root, "s")
	n := getElementById(doc.Root, "n")
	layouts := 0
	eng := New()
	eng.SetLayoutUpdater(func() map[*html.Node]*layout.Box {
		layouts++
		// Stand-in layout: clamps #s's requested offset to [0, 50].
		off := s.ScrollOffset
		off.Y = max(0, min(off.Y, 50))
		return map[*html.Node]*layout.Box{s: {Node: s, ScrollOffset: off}}
	})
	doc.Scripts = append(doc.Scripts, `
		var s = document.getElementById("s");
		s.scrollTop = 100;
		if (s.scrollTop !== 50) throw new Error("scrollTop = " + s.scrollTop);
		var n = document.getElementById("n");
		n.scrollTop = 10;
		if (n.scrollTop !== 0) throw new Error("box-less scrollTop = " + n.scrollTop);
	`)
	if err := eng.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if s.ScrollOffset != (html.ScrollOffset{Y: 50}) {
		t.Errorf("s.ScrollOffset = %+v, want {0 50}", s.ScrollOffset)
	}
	if n.ScrollOffset != (html.ScrollOffset{}) {
		t.Errorf("n.ScrollOffset = %+v, want zero", n.ScrollOffset)
	}
	if layouts == 0 {
		t.Error("scroll bindings never updated layout")
	}
}

// TestElementScrollTo pins Element.scrollTo(x, y) and scrollTo({left,
// top}) (Element::scrollTo → ScrollLayoutBoxTo, dom/element.cc:3011-3046,
// 3110-3175 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): both set the
// scroll offset, an option left out keeps that axis, and the result is
// clamped by layout like scrollLeft/scrollTop.
func TestElementScrollTo(t *testing.T) {
	doc := parseHTML(t, `<div id="s"></div>`)
	s := getElementById(doc.Root, "s")
	eng := New()
	eng.SetLayoutUpdater(func() map[*html.Node]*layout.Box {
		off := s.ScrollOffset
		off.X = max(0, min(off.X, 50))
		off.Y = max(0, min(off.Y, 50))
		return map[*html.Node]*layout.Box{s: {Node: s, ScrollOffset: off}}
	})
	doc.Scripts = append(doc.Scripts, `
		var s = document.getElementById("s");
		s.scrollTo(10, 20);
		if (s.scrollLeft !== 10 || s.scrollTop !== 20) throw new Error("scrollTo(x,y) = " + s.scrollLeft + "," + s.scrollTop);
		s.scrollTo({top: 30});
		if (s.scrollLeft !== 10 || s.scrollTop !== 30) throw new Error("scrollTo({top}) = " + s.scrollLeft + "," + s.scrollTop);
		s.scrollTo({left: 400, top: -5});
		if (s.scrollLeft !== 50 || s.scrollTop !== 0) throw new Error("clamped = " + s.scrollLeft + "," + s.scrollTop);
	`)
	if err := eng.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if s.ScrollOffset != (html.ScrollOffset{X: 50}) {
		t.Errorf("ScrollOffset = %+v, want {50 0}", s.ScrollOffset)
	}
}

// TestHasScriptWork pins that a document whose only script is a
// <body onload="..."> handler still needs Execute: the handler fires on
// the window's load event with no <script> in the document.
func TestHasScriptWork(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`<body><p>x</p></body>`, false},
		{`<body onload="document.body.id='x'"><p>x</p></body>`, true},
		{`<body><script>var a = 1;</script></body>`, true},
	} {
		if got := HasScriptWork(parseHTML(t, tc.src)); got != tc.want {
			t.Errorf("HasScriptWork(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

// TestScrollingElementScrollsViewport pins the document's scrolling
// element: the document element in standards mode (Document::
// ScrollingElementNoLayout, dom/document.cc:2051-2066 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b — louis14 has no quirks mode),
// whose scrollLeft / scrollTop / scrollWidth / scrollHeight act on the
// layout viewport instead of its own box (Element::scrollLeft,
// setScrollLeft, scrollWidth, scrollHeight, dom/element.cc:2724-2975 @
// a9f50e522efa): the setter scrolls the window (stored on the Document
// node, the LayoutView's node), layout clamps it, and scrollWidth /
// scrollHeight report the viewport's contents size.
func TestScrollingElementScrollsViewport(t *testing.T) {
	doc := parseHTML(t, `<!DOCTYPE html><html><body></body></html>`)
	eng := New()
	eng.SetLayoutUpdater(func() map[*html.Node]*layout.Box {
		// Stand-in layout: a 1072x600 document in an 800x600 viewport.
		off := doc.Root.ScrollOffset
		off.X = max(0, min(off.X, 272))
		off.Y = 0
		return map[*html.Node]*layout.Box{doc.Root: {
			ScrollOffset: off,
			ScrollSize:   layout.PhysicalSize{Width: 1072, Height: 600},
		}}
	})
	doc.Scripts = append(doc.Scripts, `
		var se = document.scrollingElement;
		if (se !== document.documentElement) throw new Error("scrollingElement is not the document element");
		if (se.scrollWidth !== 1072 || se.scrollHeight !== 600) throw new Error("scroll size = " + se.scrollWidth + "x" + se.scrollHeight);
		se.scrollLeft = se.scrollWidth;
		if (se.scrollLeft !== 272) throw new Error("scrollLeft = " + se.scrollLeft);
		se.scrollTop = 50;
		if (se.scrollTop !== 0) throw new Error("scrollTop = " + se.scrollTop);
	`)
	if err := eng.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if want := (html.ScrollOffset{X: 272}); doc.Root.ScrollOffset != want {
		t.Errorf("Document ScrollOffset = %+v, want %+v", doc.Root.ScrollOffset, want)
	}
}

// TestElementGetClientRects pins Element::getClientRects
// (core/dom/element.cc:3455-3487 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b;
// element.idl:147): it brings layout up to date first
// (GetClientRectsNoAdjustment → EnsurePaintLocationDataValidForNode), so a
// style change made just before the call is reflected; an element with a
// box reports its border box as a DOMRectList of one DOMRect; a box-less
// element (display:contents here) reports an empty list.
func TestElementGetClientRects(t *testing.T) {
	doc := parseHTML(t, `<div id="s"></div>`)
	s := getElementById(doc.Root, "s")
	layouts := 0
	eng := New()
	eng.SetLayoutUpdater(func() map[*html.Node]*layout.Box {
		layouts++
		// Stand-in layout: display:contents generates no box.
		if style, _ := s.GetAttribute("style"); style == "display:contents" {
			return map[*html.Node]*layout.Box{}
		}
		return map[*html.Node]*layout.Box{s: {Node: s, X: 10, Y: 20, Width: 30, Height: 40}}
	})
	doc.Scripts = append(doc.Scripts, `
		var s = document.getElementById("s");
		var rects = s.getClientRects();
		if (rects.length !== 1) throw new Error("length = " + rects.length);
		var r = rects[0];
		if (rects.item(0) !== r) throw new Error("item(0) !== [0]");
		if (rects.item(1) !== null) throw new Error("item(1) should be null");
		var want = {x: 10, y: 20, width: 30, height: 40, left: 10, top: 20, right: 40, bottom: 60};
		for (var k in want) if (r[k] !== want[k]) throw new Error(k + " = " + r[k]);
		s.setAttribute("style", "display:contents");
		if (s.getClientRects().length !== 0) throw new Error("box-less element should have no rects");
	`)
	if err := eng.Execute(doc); err != nil {
		t.Fatal(err)
	}
	if layouts != 2 {
		t.Errorf("getClientRects ran layout %d times, want 2", layouts)
	}
}
