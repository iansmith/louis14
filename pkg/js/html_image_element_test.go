package js

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"testing"
)

func pngDataURL(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// runImageScript runs script in a document with a <div id="log">, and
// returns the log's text: the order the script and image handlers ran in.
func runImageScript(t *testing.T, engine *Engine, script string) string {
	t.Helper()
	doc := parseHTML(t, `<div id="log"></div>`)
	doc.Scripts = append(doc.Scripts, `function log(s) { document.getElementById("log").textContent += s + ";"; }
`+script)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	return getTextContent(getElementById(doc.Root, "log"))
}

// new Image(width, height) is an <img> not created by the parser whose
// width/height content attributes are set (HTMLImageElement::
// CreateForJSConstructor, core/html/html_image_element.cc:149-165 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestImageConstructor(t *testing.T) {
	got := runImageScript(t, New(), `
		var a = new Image();
		log(a.tagName + "," + a.getAttribute("width") + "," + a.parentNode);
		var b = new Image(12);
		log(b.getAttribute("width") + "," + b.getAttribute("height"));
		var c = new Image(12, 7);
		log(c.getAttribute("width") + "," + c.getAttribute("height"));
	`)
	if want := "IMG,null,null;12,null;12,7;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// The load event is dispatched asynchronously after the image loads
// (ImageLoader::DispatchPendingLoadEvent is a posted task,
// core/loader/image_loader.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// it runs after the script that set src, so a handler set after src still
// runs, and `this` is the image.
func TestImageLoadEventIsAsync(t *testing.T) {
	got := runImageScript(t, New(), fmt.Sprintf(`
		var img = new Image();
		img.src = %q;
		img.onload = function() { log("load:" + (this === img)); };
		img.onerror = function() { log("error"); };
		log("after src");
	`, pngDataURL(t, 3, 2)))
	if want := "after src;load:true;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// A resource that fails to load or decode fires error, not load; an empty
// src fires error too (ImageLoader::DoUpdateFromElement).
func TestImageErrorEvent(t *testing.T) {
	engine := New()
	engine.SetImageFetcher(func(uri string) ([]byte, error) { return nil, fmt.Errorf("404: %s", uri) })
	got := runImageScript(t, engine, `
		var a = new Image();
		a.onload = function() { log("a load"); };
		a.onerror = function() { log("a error"); };
		a.src = "missing.png";
		var b = new Image();
		b.onerror = function() { log("b error"); };
		b.src = "";
	`)
	if want := "a error;b error;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// The image fetcher set on the engine serves the load, and a second src
// assignment before the event runs replaces the first load (only the
// latest request dispatches).
func TestImageLoadUsesEngineFetcher(t *testing.T) {
	data := pngDataURL(t, 4, 4)
	engine := New()
	engine.SetImageFetcher(func(uri string) ([]byte, error) {
		if uri == "ok.png" {
			b, _ := base64.StdEncoding.DecodeString(data[len("data:image/png;base64,"):])
			return b, nil
		}
		return nil, fmt.Errorf("404: %s", uri)
	})
	got := runImageScript(t, engine, `
		var img = new Image();
		img.onload = function() { log("load " + img.getAttribute("src")); };
		img.onerror = function() { log("error " + img.getAttribute("src")); };
		img.src = "missing.png";
		img.src = "ok.png";
	`)
	if want := "load ok.png;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// The body onload handler that sets an image's src (the css-images *c
// tests' shape) still sees that image's load event.
func TestImageLoadFromBodyOnload(t *testing.T) {
	doc := parseHTML(t, `<body onload="go()"><div id="log"></div></body>`)
	doc.Scripts = append(doc.Scripts, fmt.Sprintf(`
		function log(s) { document.getElementById("log").textContent += s + ";"; }
		function go() {
			var img = new Image();
			img.onload = function() { log("load"); };
			img.src = %q;
			log("onload");
		}
	`, pngDataURL(t, 2, 2)))
	if err := New().Execute(doc); err != nil {
		t.Fatal(err)
	}
	if got, want := getTextContent(getElementById(doc.Root, "log")), "onload;load;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// A parsed <img> whose load handler a script sets gets its load event after
// that script, and before the window load event, which waits for the
// document's images (Document load-event delay).
func TestImageParsedLoadBeforeWindowLoad(t *testing.T) {
	doc := parseHTML(t, fmt.Sprintf(`<div id="log"></div><img id="i" src=%q>`, pngDataURL(t, 2, 2)))
	doc.Scripts = append(doc.Scripts, `
		function log(s) { document.getElementById("log").textContent += s + ";"; }
		document.getElementById("i").onload = function() { log("img load"); };
		window.onload = function() { log("window load"); };
		log("script");
	`)
	if err := New().Execute(doc); err != nil {
		t.Fatal(err)
	}
	if got, want := getTextContent(getElementById(doc.Root, "log")), "script;img load;window load;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// new Image(w, h) converts its arguments with WebIDL ToUint32 (unsigned
// long): non-finite → 0, truncate, modulo 2^32 (#355 review).
func TestImageConstructorToUint32(t *testing.T) {
	got := runImageScript(t, New(), `
		var a = new Image(Infinity, NaN);
		log(a.getAttribute("width") + "," + a.getAttribute("height"));
		var b = new Image(-1, 4294967297.9);
		log(b.getAttribute("width") + "," + b.getAttribute("height"));
	`)
	if want := "0,0;4294967295,1;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// Removing src makes a null source: no error event of its own, the pending
// load event is canceled, and an error event already queued by the previous
// request still fires (ImageLoader::DoUpdateFromElement,
// core/loader/image_loader.cc:585-628 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) (#355 review).
func TestImageRemoveSrc(t *testing.T) {
	engine := New()
	engine.SetImageFetcher(func(uri string) ([]byte, error) { return nil, fmt.Errorf("404: %s", uri) })
	got := runImageScript(t, engine, fmt.Sprintf(`
		var ok = new Image();
		ok.onload = function() { log("ok load"); };
		ok.onerror = function() { log("ok error"); };
		ok.src = %q;
		ok.removeAttribute("src");
		var bad = new Image();
		bad.onload = function() { log("bad load"); };
		bad.onerror = function() { log("bad error"); };
		bad.src = "missing.png";
		bad.removeAttribute("src");
	`, pngDataURL(t, 2, 2)))
	if want := "bad error;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// An <img> inside <template> content is inert: its load never starts
// (#355 review).
func TestImageInTemplateIsInert(t *testing.T) {
	doc := parseHTML(t, fmt.Sprintf(`<div id="log"></div><template><img src=%q></template>`, pngDataURL(t, 2, 2)))
	doc.Scripts = append(doc.Scripts, `
		function log(s) { document.getElementById("log").textContent += s + ";"; }
		var t = document.getElementsByTagName("template")[0];
		var img = t.getElementsByTagName("img")[0] || t.firstChild;
		if (img) img.onload = function() { log("template img load"); };
		log("script");
	`)
	if err := New().Execute(doc); err != nil {
		t.Fatal(err)
	}
	if got, want := getTextContent(getElementById(doc.Root, "log")), "script;"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// Non-img load handlers fire in the order they were set, not in map
// order (#355 review).
func TestElementLoadHandlersInRegistrationOrder(t *testing.T) {
	var want string
	script := ""
	for i := 0; i < 12; i++ {
		script += fmt.Sprintf(`document.createElement("iframe").onload = function() { log("%d"); };`+"\n", i)
		want += fmt.Sprintf("%d;", i)
	}
	for run := 0; run < 5; run++ {
		if got := runImageScript(t, New(), script); got != want {
			t.Fatalf("run %d: log = %q, want %q", run, got, want)
		}
	}
}
