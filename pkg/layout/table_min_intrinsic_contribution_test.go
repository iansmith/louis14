package layout

import "testing"

// A table never contributes less than its min-intrinsic inline size, even
// when its own inline-size or max-inline-size is smaller: Blink's
// ComputeMinMaxInlineSizes ends with "Tables can't shrink below their
// min-intrinsic size" (length_utils.cc:793-796 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Here the table's min-intrinsic
// size is its 60px content plus 10px + 10px of borders; with its 10px + 10px
// margins the min-content parent is 100px wide
// (css-tables/table-intrinsic-size-001, -002).
func TestTableContributionEncompassesMinIntrinsic(t *testing.T) {
	for _, c := range []struct{ name, size string }{
		{"inline-size", "inline-size: 30px"},
		{"max-inline-size", "max-inline-size: 30px"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := `<!DOCTYPE html><body style="margin:0"><div id="outer" style="width:min-content">` +
				`<div style="display:table; ` + c.size + `; border-inline:10px solid; margin-inline:10px">` +
				`<div style="inline-size:60px; height:10px"></div></div></div>`
			outer := findBoxByID(layoutRootTestHTML(t, src), "outer")
			if outer == nil {
				t.Fatal("no #outer box")
			}
			if outer.Width != 100 {
				t.Errorf("min-content parent width = %v, want 100 (table min-intrinsic 80 + 20 margins)", outer.Width)
			}
		})
	}
}
