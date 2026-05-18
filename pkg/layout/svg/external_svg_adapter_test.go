package svg

import (
	"testing"
)

func TestParseExternalSVG_Basic(t *testing.T) {
	data := []byte(`<svg xmlns="http://www.w3.org/2000/svg">` +
		`<filter id="f" x="-10%" y="-10%" width="120%" height="120%">` +
		`<feFlood flood-color="green"/></filter></svg>`)
	root, err := ParseExternalSVG(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if root.TagName() != "svg" {
		t.Errorf("root tag = %q, want svg", root.TagName())
	}
	if len(root.SVGChildren()) != 1 {
		t.Fatalf("children = %d, want 1", len(root.SVGChildren()))
	}
	filt := root.SVGChildren()[0]
	if filt.TagName() != "filter" {
		t.Errorf("child tag = %q, want filter", filt.TagName())
	}
	if id, ok := filt.Attribute("id"); !ok || id != "f" {
		t.Errorf("id = %q, ok=%v", id, ok)
	}
	if v, ok := filt.Attribute("x"); !ok || v != "-10%" {
		t.Errorf("x = %q", v)
	}
	flood := filt.SVGChildren()[0]
	if flood.TagName() != "feflood" {
		t.Errorf("primitive tag = %q", flood.TagName())
	}
	if c, ok := flood.Attribute("flood-color"); !ok || c != "green" {
		t.Errorf("flood-color = %q", c)
	}
}

func TestParseExternalSVG_FindByID(t *testing.T) {
	data := []byte(`<svg xmlns="http://www.w3.org/2000/svg">` +
		`<defs>` +
		`<filter id="A"><feFlood flood-color="red"/></filter>` +
		`<filter id="B"><feFlood flood-color="green"/></filter>` +
		`</defs></svg>`)
	root, err := ParseExternalSVG(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, id := range []string{"A", "B"} {
		got, ok := FindElementByID(root, id)
		if !ok {
			t.Fatalf("could not find %q", id)
		}
		if got.TagName() != "filter" {
			t.Errorf("found %q tag = %q", id, got.TagName())
		}
	}
	if _, ok := FindElementByID(root, "missing"); ok {
		t.Error("expected miss for unknown id")
	}
}

func TestParseExternalSVG_CamelCaseAttr(t *testing.T) {
	data := []byte(`<svg><filter id="f" filterUnits="userSpaceOnUse"/></svg>`)
	root, err := ParseExternalSVG(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	filt := root.SVGChildren()[0]
	// Both camelCase and lowercased lookups must work — matches the
	// fallback pattern in NewSVGResourceFilter.
	if v, ok := filt.Attribute("filterUnits"); !ok || v != "userSpaceOnUse" {
		t.Errorf("filterUnits = %q, ok=%v", v, ok)
	}
	if v, ok := filt.Attribute("filterunits"); !ok || v != "userSpaceOnUse" {
		t.Errorf("filterunits = %q, ok=%v", v, ok)
	}
}

func TestParseExternalSVG_DataURLInlineFromBlurTest(t *testing.T) {
	// The blur-text WPT test's data URL payload.
	data := []byte(`<svg xmlns='http://www.w3.org/2000/svg'><filter id='blur'><feGaussianBlur stdDeviation='4' /></filter></svg>`)
	root, err := ParseExternalSVG(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	filt, ok := FindElementByID(root, "blur")
	if !ok {
		t.Fatal("could not find #blur")
	}
	if filt.TagName() != "filter" {
		t.Fatalf("tag = %q", filt.TagName())
	}
	prim := filt.SVGChildren()[0]
	if prim.TagName() != "fegaussianblur" {
		t.Errorf("primitive = %q", prim.TagName())
	}
	if v, ok := prim.Attribute("stdDeviation"); !ok || v != "4" {
		t.Errorf("stdDeviation = %q, ok=%v", v, ok)
	}
}

func TestParseExternalSVG_Empty(t *testing.T) {
	_, err := ParseExternalSVG([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}
