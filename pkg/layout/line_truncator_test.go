package layout

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// paintedTextRuns formats the text boxes under b that paint, in physical x
// order, as `"text"@x+width`, marking the ellipsis. A box hidden for paint
// (a child the line truncator hid) is skipped with its subtree.
func paintedTextRuns(b *Box) string {
	type run struct {
		x float64
		s string
	}
	var runs []run
	var walk func(*Box)
	walk = func(b *Box) {
		if b.IsHiddenForPaint {
			return
		}
		if b.Text != "" {
			f := fmt.Sprintf("%q@%g+%g", b.Text, b.X, b.Width)
			if b.IsEllipsis {
				f += " ellipsis"
			}
			runs = append(runs, run{b.X, f})
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(b)
	sort.Slice(runs, func(i, j int) bool { return runs[i].x < runs[j].x })
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.s
	}
	return strings.Join(out, " ")
}

// TestLineTruncator_TruncateLine checks that a line overflowing a scroll
// container with text-overflow other than clip is truncated in layout, as
// Blink's LineTruncator::TruncateLine does (inline/line_truncator.cc:173-240
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the children past the
// ellipsis are hidden for paint, the child the ellipsis follows is truncated
// to fit, and the ellipsis is placed next to it in the line's style.
//
// Ahem maps U+2026 to a 1em square, so the "…" is 100px wide here.
func TestLineTruncator_TruncateLine(t *testing.T) {
	const block = "font:100px/1 Ahem;width:400px;white-space:pre;overflow:hidden;text-overflow:ellipsis"
	cases := []struct {
		name       string
		tag        string // the block's element; div when empty
		style      string
		body       string
		want       string
		wantHidden []string // ids of boxes the truncator must hide
	}{
		{
			name: "ltr: the ellipsis follows the truncated text",
			body: "ppppp",
			want: `"ppp"@0+300 "…"@300+100 ellipsis`,
		},
		{
			name:  "rtl: the ellipsis precedes the truncated text on the left",
			style: "direction:rtl",
			body:  "ppppp",
			want:  `"…"@0+100 ellipsis "ppp"@100+300`,
		},
		{
			name:  "a <string> value replaces the ellipsis",
			style: `text-overflow:"XX"`,
			body:  "ppppp",
			want:  `"pp"@0+200 "XX"@200+200 ellipsis`,
		},
		{
			name:  "a centered line is truncated from the start edge (LOU-834)",
			style: "text-align:center",
			body:  "ppppp",
			want:  `"ppp"@0+300 "…"@300+100 ellipsis`,
		},
		{
			name:  "a <button>, centered by the UA, is truncated from its start edge (#421 review)",
			tag:   "button",
			style: "border:0;padding:0",
			body:  "ppppp",
			want:  `"ppp"@0+300 "…"@300+100 ellipsis`,
		},
		{
			name: "a line that fits is not truncated",
			body: "pppp",
			want: `"pppp"@0+400`,
		},
		{
			name:  "no truncation when the block is not a scroll container",
			style: "overflow:visible",
			body:  "ppppp",
			want:  `"ppppp"@0+500`,
		},
		{
			name:  "text-overflow:clip does not truncate",
			style: "text-overflow:clip",
			body:  "ppppp",
			want:  `"ppppp"@0+500`,
		},
		{
			name:       "an atomic inline that cannot fit beside the ellipsis is hidden",
			body:       `pp<span id=ib style="display:inline-block;width:150px;height:10px"></span>pp`,
			want:       `"pp"@0+200 "…"@200+100 ellipsis`,
			wantHidden: []string{"ib"},
		},
		{
			name: "a ruby base is a child the ellipsis can follow",
			body: `pp<ruby>X<rt>Y</rt></ruby>pp`,
			want: `"pp"@0+200 "X"@200+100 "Y"@225+50 "…"@300+100 ellipsis`,
		},
		{
			name: "the ellipsis is placed after the last child that fits",
			body: `pp<span style="color:red">pp</span>pp`,
			want: `"pp"@0+200 "p"@200+100 "…"@300+100 ellipsis`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tag := tc.tag
			if tag == "" {
				tag = "div"
			}
			src := fmt.Sprintf(`<!doctype html><style>body{margin:0}#t{%s;%s}</style><%s id=t>%s</%s>`,
				block, tc.style, tag, tc.body, tag)
			root := layoutRootTestHTML(t, src)
			div := findBoxByID(root, "t")
			if div == nil {
				t.Fatal("no box for #t")
			}
			if got := paintedTextRuns(div); got != tc.want {
				t.Errorf("painted text runs:\n got %s\nwant %s", got, tc.want)
			}
			for _, id := range tc.wantHidden {
				if b := findBoxByID(root, id); b == nil || !b.IsHiddenForPaint {
					t.Errorf("#%s past the ellipsis is not hidden for paint", id)
				}
			}
		})
	}
}
