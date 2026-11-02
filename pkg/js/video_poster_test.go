package js

import "testing"

// HTMLVideoElement.poster reflects the poster content attribute
// (html_video_element.idl @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the
// css-images object-view-box-*-video tests set it from script (LOU-619).
func TestVideoPosterReflects(t *testing.T) {
	doc := parseHTML(t, `<video id="v"></video><div id="d"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var v = document.getElementById("v");
		if (v.poster !== "") throw new Error("initial poster: " + v.poster);
		v.poster = "support/p.jpg";
		if (v.getAttribute("poster") !== "support/p.jpg") throw new Error("attribute: " + v.getAttribute("poster"));
		if (v.poster !== "support/p.jpg") throw new Error("poster: " + v.poster);
		var d = document.getElementById("d");
		d.poster = "x";
		if (d.getAttribute("poster") !== null) throw new Error("non-video poster reflected");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
