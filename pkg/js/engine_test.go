package js

import (
	"testing"

	"louis14/pkg/html"
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
