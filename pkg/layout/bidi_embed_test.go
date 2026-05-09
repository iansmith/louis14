package layout

import (
	"fmt"
	"testing"

	xbidi "golang.org/x/text/unicode/bidi"
)

func TestEmbeddingLevelsDeep(t *testing.T) {
	// Text with LRE + RLE nesting to create level 2 and 3
	// ab LRE cd RLE 12 PDF ef PDF gh
	// LRE = U+202A, RLE = U+202B, PDF = U+202C
	text := "ab‪cd‫12‬ef‬gh"
	runes := []rune(text)

	classes := make([]xbidi.Class, len(runes))
	for i, r := range runes {
		props, _ := xbidi.LookupRune(r)
		classes[i] = props.Class()
	}

	baseLevel := 0
	embLevels := computeEmbeddingLevels(runes, baseLevel, classes)

	for i, r := range runes {
		t.Logf("rune[%d] = %U '%c', class=%v, embLevel=%d", i, r, r, classes[i], embLevels[i])
	}

	// Expected:
	// a (LTR) → level 0
	// b (LTR) → level 0
	// LRE → level 0, opens level 2 (next even after 0|1+1 = 2)
	// c (LTR) → level 2
	// d (LTR) → level 2
	// RLE → level 2, opens level 3 (next odd after 2+1|1 = 3)
	// 1 (EN) → level 3
	// 2 (EN) → level 3
	// PDF → level 3, pop to level 2
	// e (LTR) → level 2
	// f (LTR) → level 2
	// PDF → level 2, pop to level 0
	// g (LTR) → level 0
	// h (LTR) → level 0

	embeddings := make([]int, len(runes))
	for i := range runes {
		embeddings[i] = embLevels[i]
	}
	fmt.Printf("Embedding levels: %v\n", embeddings)

	if embLevels[2] != 0 || embLevels[3] != 2 {
		t.Errorf("LRE at pos 2 should set level 0, content at pos 3 should be level 2; got %d, %d", embLevels[2], embLevels[3])
	}
	if embLevels[6] != 3 {
		t.Errorf("Inside RLE at pos 6, expected level 3, got %d", embLevels[6])
	}
	// PDF at pos 8 gets the current embedding level (3 = RLE level)
	if embLevels[8] != 3 {
		t.Errorf("PDF at pos 8 should be at RLE level 3, got %d", embLevels[8])
	}
	// After PDF, we pop to LRE level 2
	if embLevels[9] != 2 {
		t.Errorf("After PDF at pos 9, expected level 2, got %d", embLevels[9])
	}
}

func TestResolveBidiLevelsProducesLevelsBeyond01(t *testing.T) {
	// Test the full ResolveBidiLevels pipeline with embedded text.
	// Text: "abc LRE def PDF ghi"
	// The "def" should get embedding level 2.
	text := "abc‪def‬ghi"
	itemsData := &InlineItemsData{
		TextContent: text,
		Items: []*InlineItem{
			{
				Type:        InlineItemText,
				StartOffset: 0,
				EndOffset:   len(text),
			},
		},
	}

	ResolveBidiLevels(itemsData, DirectionLTR)

	if len(itemsData.RuneLevels) == 0 {
		t.Fatal("RuneLevels should be populated")
	}

	runes := []rune(text)
	for i := range runes {
		t.Logf("rune[%d] = %U '%c', level=%d", i, runes[i], runes[i], itemsData.RuneLevels[i])
	}

	// "abc" should be level 0
	for i := 0; i < 3; i++ {
		if itemsData.RuneLevels[i] != 0 {
			t.Errorf("rune %d ('a') expected level 0, got %d", i, itemsData.RuneLevels[i])
		}
	}

	// "def" should be level 2 (embedded in LTR)
	for i := 4; i < 7; i++ {
		if itemsData.RuneLevels[i] != 2 {
			t.Errorf("rune %d ('d') expected level 2, got %d", i, itemsData.RuneLevels[i])
		}
	}

	// "ghi" should be level 0
	for i := 8; i < 11; i++ {
		if itemsData.RuneLevels[i] != 0 {
			t.Errorf("rune %d ('g') expected level 0, got %d", i, itemsData.RuneLevels[i])
		}
	}
}
