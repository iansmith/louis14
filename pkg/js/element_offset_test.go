package js

import (
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// runWithLayout runs script against doc with a layout updater that lays the
// document out again on each call, as the reftest harness does.
func runWithLayout(t *testing.T, page, script string) {
	t.Helper()
	doc := parseHTML(t, page)
	eng := New()
	eng.SetLayoutUpdater(func() map[*html.Node]*layout.Box {
		e := layout.NewLayoutEngine(800, 600)
		e.Layout(doc)
		return e.NodeBoxes()
	})
	doc.Scripts = append(doc.Scripts, script)
	if err := eng.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// Element.offsetParent mirrors Element::OffsetParent → LayoutObject::
// OffsetParent (dom/element.cc:2524-2530; layout/layout_object.cc:4626-4697 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): null for the document element
// and body; otherwise the nearest layout ancestor that can contain absolutely
// positioned objects (fixed ones for a fixed element, null when none), body,
// or for a non-positioned element a table or table cell.
func TestElementOffsetParent(t *testing.T) {
	runWithLayout(t, `<!DOCTYPE html><body>
<div id="rel" style="position:relative"><div id="mid"><div id="abs" style="position:absolute"></div><div id="flow"></div></div></div>
<div id="static"><p id="p"></p></div>
<div id="fixed" style="position:fixed"></div>
<table><tr><td id="td"><span id="span" style="display:block"></span></td></tr></table>
</body>`, `
		function id(s) { return document.getElementById(s); }
		function expect(name, got, want) { if (got !== want) throw new Error(name + ": got " + (got && got.id || got) + ", want " + (want && want.id || want)); }
		expect("html", document.documentElement.offsetParent, null);
		expect("body", document.body.offsetParent, null);
		expect("abs", id("abs").offsetParent, id("rel"));
		expect("flow", id("flow").offsetParent, id("rel"));
		expect("p", id("p").offsetParent, document.body);
		expect("fixed", id("fixed").offsetParent, null);
		expect("in a cell", id("span").offsetParent, id("td"));
	`)
}

// offsetLeft/offsetTop are the border-box offset from the offsetParent's
// padding edge, or from the canvas when the offsetParent is a non-positioned
// body or null; offsetWidth/offsetHeight are the border-box size, rounded
// (Element::OffsetLeft … OffsetHeight, dom/element.cc:2477-2522;
// LayoutBoxModelObject::AdjustedPositionRelativeTo,
// layout/layout_box_model_object.cc:754-812 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestElementOffsetGeometry(t *testing.T) {
	runWithLayout(t, `<!DOCTYPE html><body style="margin:8px">
<div id="rel" style="position:relative;margin-left:30px;border:5px solid;width:200px;height:100px">
<div id="abs" style="position:absolute;left:10px;top:20px;width:40.4px;height:30px;border:2px solid"></div></div>
</body>`, `
		function id(s) { return document.getElementById(s); }
		function expect(name, got, want) { if (got !== want) throw new Error(name + ": got " + got + ", want " + want); }
		expect("abs.offsetLeft", id("abs").offsetLeft, 10);
		expect("abs.offsetTop", id("abs").offsetTop, 20);
		expect("abs.offsetWidth", id("abs").offsetWidth, 44);
		expect("abs.offsetHeight", id("abs").offsetHeight, 34);
		expect("rel.offsetLeft", id("rel").offsetLeft, 38);
		expect("rel.offsetTop", id("rel").offsetTop, 8);
		expect("rel.offsetWidth", id("rel").offsetWidth, 210);
	`)
}

// Scrolling does not move an element's offsets: Blink sums unscrolled
// locations, while louis14's boxes inside a scroller are translated by its
// scroll offset.
func TestElementOffsetIgnoresScroll(t *testing.T) {
	runWithLayout(t, `<!DOCTYPE html><body style="margin:0">
<div id="sc" style="position:relative;overflow:auto;width:100px;height:100px">
<div style="height:50px"></div><div id="child" style="height:500px"></div></div>
</body>`, `
		var sc = document.getElementById("sc"), child = document.getElementById("child");
		sc.scrollTop = 40;
		if (sc.scrollTop !== 40) throw new Error("scrollTop = " + sc.scrollTop);
		if (child.offsetTop !== 50) throw new Error("offsetTop = " + child.offsetTop);
	`)
}
