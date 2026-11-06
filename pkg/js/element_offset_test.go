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
// floored scroll offset (LayoutBox::ScrolledContentOffset, layout_box.cc:
// 1781-1787 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which is added
// back; a fractional scrollTop included.
func TestElementOffsetIgnoresScroll(t *testing.T) {
	for _, scroll := range []string{"40", "40.5"} {
		t.Run(scroll, func(t *testing.T) {
			runWithLayout(t, `<!DOCTYPE html><body style="margin:0">
<div id="sc" style="position:relative;overflow:auto;width:100px;height:100px">
<div style="height:50px"></div><div id="child" style="height:500px"></div></div>
</body>`, `
				var sc = document.getElementById("sc"), child = document.getElementById("child");
				sc.scrollTop = `+scroll+`;
				if (sc.scrollTop !== `+scroll+`) throw new Error("scrollTop = " + sc.scrollTop);
				if (child.offsetTop !== 50) throw new Error("offsetTop = " + child.offsetTop);
			`)
		})
	}
}

// The document element has no offsetParent and its offsets are its location
// in the LayoutView (AdjustedPositionRelativeTo returns zero only for body
// or an object without a layout parent).
func TestElementOffsetDocumentElement(t *testing.T) {
	runWithLayout(t, `<!DOCTYPE html><html style="margin:20px 0 0 30px"><body></body></html>`, `
		var html = document.documentElement;
		if (html.offsetParent !== null) throw new Error("offsetParent = " + html.offsetParent);
		if (html.offsetTop !== 20) throw new Error("offsetTop = " + html.offsetTop);
		if (html.offsetLeft !== 30) throw new Error("offsetLeft = " + html.offsetLeft);
	`)
}

// A transformed document element contains fixed-position descendants, a
// filtered one does not (LayoutObject::ComputeIsFixedContainer,
// layout_object.cc:1849-1897 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a
// fixed element's offsetParent is the document element, or null.
func TestElementOffsetParentFixedUnderRoot(t *testing.T) {
	for _, tc := range []struct{ root, want string }{
		{"transform:translateX(0)", "document.documentElement"},
		{"filter:invert(0)", "null"},
		{"", "null"},
	} {
		t.Run(tc.root, func(t *testing.T) {
			runWithLayout(t, `<!DOCTYPE html><html style="`+tc.root+`"><body><div id="f" style="position:fixed"></div></body></html>`, `
			var got = document.getElementById("f").offsetParent;
			if (got !== `+tc.want+`) throw new Error("`+tc.root+`: offsetParent = " + (got && got.tagName));
		`)
		})
	}
}

// Offsets are LayoutUnits rounded as LayoutUnit::Round does, halves toward
// +infinity (layout_unit.h:294-297 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): -0.5px reads 0, a 10.5px width
// 11.
func TestElementOffsetRounding(t *testing.T) {
	runWithLayout(t, `<!DOCTYPE html><body style="margin:0">
<div style="position:relative;left:10px">
<div id="a" style="position:absolute;left:-0.5px;top:-1.5px;width:10.5px;height:10px"></div></div>
</body>`, `
		var a = document.getElementById("a");
		if (a.offsetLeft !== 0) throw new Error("offsetLeft = " + a.offsetLeft);
		if (a.offsetTop !== -1) throw new Error("offsetTop = " + a.offsetTop);
		if (a.offsetWidth !== 11) throw new Error("offsetWidth = " + a.offsetWidth);
	`)
}

// offsetParent and offset* are HTMLElement attributes (html_element.idl):
// an SVG element has none of them.
func TestElementOffsetHTMLOnly(t *testing.T) {
	runWithLayout(t, `<!DOCTYPE html><body><svg width="50" height="50"><rect id="r" width="10" height="10"/></svg></body>`, `
		var r = document.getElementById("r");
		["offsetParent", "offsetLeft", "offsetTop", "offsetWidth", "offsetHeight"].forEach(function (k) {
			if (r[k] !== undefined) throw new Error("svg rect " + k + " = " + r[k]);
		});
	`)
}
