package css

import (
	"testing"

	"louis14/pkg/html"
)

// LOU-480: orphans / widows (CSS Fragmentation 3 §4.4) are inherited
// `<integer [1,∞]>` properties with initial value 2. Mirrors Blink's
// css_properties.json5 `orphans` / `widows` entries (inherited, initial 2,
// ConsumePositiveInteger) @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.

func orphansWidowsStyles(t *testing.T, src string) map[string]*Style {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	styles := ApplyStylesToDocument(doc, 800, 600)
	byID := map[string]*Style{}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if id := n.Attributes["id"]; n.Type == html.ElementNode && id != "" {
			byID[id] = styles[n]
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	return byID
}

func TestOrphansWidows_InitialIsTwo(t *testing.T) {
	s := orphansWidowsStyles(t, `<div id="a">x</div>`)["a"]
	if got := s.GetOrphans(); got != 2 {
		t.Errorf("initial orphans = %d, want 2", got)
	}
	if got := s.GetWidows(); got != 2 {
		t.Errorf("initial widows = %d, want 2", got)
	}
}

func TestOrphansWidows_Inherited(t *testing.T) {
	s := orphansWidowsStyles(t, `<div style="orphans:3; widows:4"><p id="c">x</p></div>`)["c"]
	if got := s.GetOrphans(); got != 3 {
		t.Errorf("inherited orphans = %d, want 3", got)
	}
	if got := s.GetWidows(); got != 4 {
		t.Errorf("inherited widows = %d, want 4", got)
	}
}

// An invalid value (zero, negative, non-integer) is dropped at parse time,
// so an earlier valid declaration survives.
func TestOrphansWidows_InvalidValueDropped(t *testing.T) {
	styles := orphansWidowsStyles(t, `<style>
		#z { orphans: 5; orphans: 0; widows: 5; widows: -1; }
		#f { orphans: 5; orphans: 1.5; widows: 5; widows: auto; }
	</style><div id="z">x</div><div id="f">x</div>`)
	for _, id := range []string{"z", "f"} {
		if got := styles[id].GetOrphans(); got != 5 {
			t.Errorf("#%s orphans = %d, want 5 (invalid later value dropped)", id, got)
		}
		if got := styles[id].GetWidows(); got != 5 {
			t.Errorf("#%s widows = %d, want 5 (invalid later value dropped)", id, got)
		}
	}
}
