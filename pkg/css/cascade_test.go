package css

import (
	"louis14/pkg/html"
	"testing"
)

func TestComputeStyle_ElementSelector(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`div { color: red; }`)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
	}

	style := ComputeStyle(node, stylesheets, 800, 600)

	if color, ok := style.Get("color"); !ok || color != "red" {
		t.Errorf("expected color='red', got '%s'", color)
	}
}

func TestComputeStyle_SpecificityOverride(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; }
		.highlight { color: blue; }
	`)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600)

	// Class selector (.highlight) should override element selector (div)
	if color, ok := style.Get("color"); !ok || color != "blue" {
		t.Errorf("expected color='blue' (class overrides element), got '%s'", color)
	}
}

func TestComputeStyle_IDHasHighestSpecificity(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; }
		.highlight { color: blue; }
		#header { color: green; }
	`)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
			"id":    "header",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600)

	// ID selector should override both class and element
	if color, ok := style.Get("color"); !ok || color != "green" {
		t.Errorf("expected color='green' (ID has highest specificity), got '%s'", color)
	}
}

func TestComputeStyle_InlineStyleOverridesAll(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; }
		.highlight { color: blue; }
		#header { color: green; }
	`)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
			"id":    "header",
			"style": "color: purple",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600)

	// Inline style should override everything
	if color, ok := style.Get("color"); !ok || color != "purple" {
		t.Errorf("expected color='purple' (inline style overrides all), got '%s'", color)
	}
}

func TestComputeStyle_MultipleProperties(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; width: 100px; }
		.highlight { color: blue; height: 50px; }
	`)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600)

	// Should have color from .highlight (overrides div)
	if color, ok := style.Get("color"); !ok || color != "blue" {
		t.Errorf("expected color='blue', got '%s'", color)
	}

	// Should have width from div (no override)
	if width, ok := style.Get("width"); !ok || width != "100px" {
		t.Errorf("expected width='100px', got '%s'", width)
	}

	// Should have height from .highlight
	if height, ok := style.Get("height"); !ok || height != "50px" {
		t.Errorf("expected height='50px', got '%s'", height)
	}
}

func TestApplyStylesToDocument(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			div { color: red; }
			.special { color: blue; }
		</style>
		<div></div>
		<div class="special"></div>
	`)

	// Phase 22: Pass viewport dimensions for media query evaluation
	styles := ApplyStylesToDocument(doc, 800, 600)

	// Should have 2 styled nodes (the divs)
	elementCount := 0
	for node, style := range styles {
		if node.Type == html.ElementNode {
			elementCount++

			// Check the style was applied
			if _, ok := style.Get("color"); !ok {
				t.Error("expected color to be set")
			}
		}
	}

	if elementCount != 2 {
		t.Errorf("expected 2 styled elements, got %d", elementCount)
	}
}

func TestInheritKeyword_FloatInherit(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.parent { float: left; }
			.child { float: inherit; }
		</style>
		<div class="parent"><div class="child"></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	// Find child node
	for node, style := range styles {
		if node.TagName == "div" {
			if cls, _ := node.GetAttribute("class"); cls == "child" {
				if val, ok := style.Get("float"); !ok || val != "left" {
					t.Errorf("expected float='left' (inherited from parent), got '%s' ok=%v", val, ok)
				}
			}
		}
	}
}

func TestInheritKeyword_ColorInherit(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.parent { color: red; }
			.child { color: inherit; }
		</style>
		<div class="parent"><span class="child"></span></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			if val, ok := style.Get("color"); !ok || val != "red" {
				t.Errorf("expected color='red' (inherited), got '%s'", val)
			}
		}
	}
}

func TestInheritKeyword_NoParentValue(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.child { float: inherit; }
		</style>
		<div class="parent"><div class="child"></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			// "inherit" should be resolved away (deleted), not left as literal "inherit"
			if val, ok := style.Get("float"); ok {
				t.Errorf("expected float to be unset (no parent value), got '%s'", val)
			}
		}
	}
}

func TestInheritKeyword_DeepNesting(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.grandparent { display: inline; }
			.parent { display: inherit; }
			.child { display: inherit; }
		</style>
		<div class="grandparent"><div class="parent"><div class="child"></div></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			if val, ok := style.Get("display"); !ok || val != "inline" {
				t.Errorf("expected display='inline' (inherited through chain), got '%s'", val)
			}
		}
	}
}

func TestInheritKeyword_InlineStyle(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.parent { color: blue; }
		</style>
		<div class="parent"><div style="color: inherit"></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if node.Parent != nil {
			if cls, _ := node.Parent.GetAttribute("class"); cls == "parent" && node.TagName == "div" {
				if val, ok := style.Get("color"); !ok || val != "blue" {
					t.Errorf("expected color='blue' (inherited via inline style), got '%s'", val)
				}
			}
		}
	}
}
