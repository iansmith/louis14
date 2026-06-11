package layout

import (
	"strings"
	"unicode/utf8"

	xbidi "golang.org/x/text/unicode/bidi"
)

// textHasBidiControls returns true if the text contains any Unicode bidi
// formatting characters (LRE, RLE, LRO, RLO, PDF, LRI, RLI, FSI, PDI).
func textHasBidiControls(text string) bool {
	for _, r := range text {
		if isBidiControlChar(r) {
			return true
		}
	}
	return false
}

// ResolveBidiLevelsSimple assigns bidi levels using the Go bidi package's
// run directions. This is the simple path for text without explicit bidi
// control characters — it only distinguishes level 0 (LTR) and level 1 (RTL).
func ResolveBidiLevelsSimple(itemsData *InlineItemsData, baseDir Direction) {
	if len(itemsData.TextContent) == 0 {
		return
	}

	defaultDir := xbidi.LeftToRight
	if baseDir == DirectionRTL {
		defaultDir = xbidi.RightToLeft
	}

	var para xbidi.Paragraph
	if _, err := para.SetString(itemsData.TextContent, xbidi.DefaultDirection(defaultDir)); err != nil {
		return
	}
	ordering, err := para.Order()
	if err != nil {
		return
	}

	textRunes := []rune(itemsData.TextContent)
	levels := make([]int, len(textRunes))
	for i := 0; i < ordering.NumRuns(); i++ {
		run := ordering.Run(i)
		start, end := run.Pos()
		lvl := 0
		if run.Direction() == xbidi.RightToLeft {
			lvl = 1
		}
		for j := start; j <= end && j < len(levels); j++ {
			levels[j] = lvl
		}
	}

	runeAtByte := make([]int, len(itemsData.TextContent)+1)
	ri := 0
	for bi := range itemsData.TextContent {
		runeAtByte[bi] = ri
		ri++
	}
	runeAtByte[len(itemsData.TextContent)] = ri

	for _, item := range itemsData.Items {
		offset := item.StartOffset
		if offset >= len(itemsData.TextContent) {
			if len(levels) > 0 {
				item.BidiLevel = levels[len(levels)-1]
			}
			continue
		}
		runeIdx := runeAtByte[offset]
		if runeIdx < len(levels) {
			item.BidiLevel = levels[runeIdx]
		}
	}
}

// ResolveBidiLevels computes per-rune resolved bidi levels using a pure-Go
// implementation of UAX#9 (X1-X8, W1-W7, N1-N2, I1-I2). This replaces
// the Go bidi package's level resolution which has issues with neutral
// character resolution.
//
// After this call, itemsData.RuneLevels is populated with per-rune resolved
// levels and each item's BidiLevel is set from the level at its start offset.
func ResolveBidiLevels(itemsData *InlineItemsData, baseDir Direction) {
	if len(itemsData.TextContent) == 0 {
		return
	}

	baseLevel := 0
	if baseDir == DirectionRTL {
		baseLevel = 1
	}

	runes := []rune(itemsData.TextContent)
	nRunes := len(runes)

	// Step 1: Get bidi class for each rune.
	classes := make([]xbidi.Class, nRunes)
	for i, r := range runes {
		props, _ := xbidi.LookupRune(r)
		classes[i] = props.Class()
	}

	// Step 2: Compute explicit embedding levels and apply overrides (UAX#9 X1-X8).
	// computeEmbeddingLevels also applies X6: when an override is active
	// (LRO/RLO), character types are forced to L/R.
	embLevels := computeEmbeddingLevels(runes, baseLevel, classes)

	// Step 3: Resolve weak types (W1-W7) and neutral types (N1-N2) within
	// each embedding level context, then apply implicit levels (I1-I2).
	levels := resolveAllLevels(classes, embLevels, baseLevel)

	// Step 4: Apply UAX#9 rule L1 — reset trailing whitespace and isolate
	// formatting characters at the end of the paragraph to the paragraph
	// embedding level. This prevents trailing spaces from participating in
	// mid-level L2 reversals.
	applyL1(levels, runes, baseLevel)

	itemsData.RuneLevels = levels

	// Set paragraph levels (uniform for non-plaintext mode).
	itemsData.ParagraphLevels = make([]int, nRunes)
	for i := range itemsData.ParagraphLevels {
		itemsData.ParagraphLevels[i] = baseLevel
	}

	// Build byte→rune index map for assigning levels to items.
	runeAtByte := make([]int, len(itemsData.TextContent)+1)
	ri := 0
	for bi := range itemsData.TextContent {
		runeAtByte[bi] = ri
		ri++
	}
	runeAtByte[len(itemsData.TextContent)] = ri

	// Assign bidi levels to each InlineItem.
	for _, item := range itemsData.Items {
		item.ParagraphLevel = baseLevel
		offset := item.StartOffset
		if offset >= len(itemsData.TextContent) {
			// Items past end of text get the paragraph embedding level,
			// matching ICU's ubidi_getLevelAt() behavior.
			item.BidiLevel = baseLevel
			continue
		}
		runeIdx := runeAtByte[offset]
		if runeIdx < nRunes {
			item.BidiLevel = levels[runeIdx]
		}
	}
}

// ResolveBidiLevelsPlaintext resolves bidi levels for unicode-bidi: plaintext
// mode. Per CSS Writing Modes §2.2, each bidi paragraph (separated by forced
// breaks / paragraph separators) independently determines its base direction
// using UAX#9 rules P2/P3 (first strong character heuristic).
//
// This mirrors Blink's NGBidiParagraph which calls ICU's ubidi_setPara with
// UBIDI_DEFAULT_LTR per paragraph when in plaintext mode.
func ResolveBidiLevelsPlaintext(itemsData *InlineItemsData) {
	text := itemsData.TextContent
	if len(text) == 0 {
		return
	}

	runes := []rune(text)
	nRunes := len(runes)

	// Get bidi class for each rune.
	allClasses := make([]xbidi.Class, nRunes)
	for i, r := range runes {
		props, _ := xbidi.LookupRune(r)
		allClasses[i] = props.Class()
	}

	allLevels := make([]int, nRunes)
	paraLevels := make([]int, nRunes)

	// Process each paragraph independently.
	// Per UAX#9 P1, paragraph boundaries are at characters with bidi class B
	// (paragraph separator, which includes \n from <br> elements).
	// The separator is kept with the preceding paragraph.
	paraStart := 0
	for paraStart < nRunes {
		// Find the end of this paragraph.
		paraEnd := paraStart
		for paraEnd < nRunes && allClasses[paraEnd] != xbidi.B {
			paraEnd++
		}
		// Include the B character in this paragraph.
		if paraEnd < nRunes {
			paraEnd++
		}

		// Determine base direction for this paragraph via P2/P3.
		// Exclude the paragraph separator itself from direction detection.
		contentEnd := paraEnd
		if contentEnd > paraStart && allClasses[contentEnd-1] == xbidi.B {
			contentEnd--
		}
		baseLevel := 0
		if contentEnd > paraStart && determineFSIDirection(runes[paraStart:contentEnd]) == 1 {
			baseLevel = 1
		}

		// Extract this paragraph's classes (copy — computeEmbeddingLevels mutates).
		paraRunes := runes[paraStart:paraEnd]
		paraClasses := make([]xbidi.Class, len(paraRunes))
		copy(paraClasses, allClasses[paraStart:paraEnd])

		// Compute embedding levels, resolve weak/neutral types, apply L1.
		embLevels := computeEmbeddingLevels(paraRunes, baseLevel, paraClasses)
		levels := resolveAllLevels(paraClasses, embLevels, baseLevel)
		applyL1(levels, paraRunes, baseLevel)

		// Store results.
		for i := 0; i < paraEnd-paraStart; i++ {
			allLevels[paraStart+i] = levels[i]
			paraLevels[paraStart+i] = baseLevel
		}

		paraStart = paraEnd
	}

	itemsData.RuneLevels = allLevels
	itemsData.ParagraphLevels = paraLevels

	// Build byte→rune index map.
	runeAtByte := make([]int, len(text)+1)
	ri := 0
	for bi := range text {
		runeAtByte[bi] = ri
		ri++
	}
	runeAtByte[len(text)] = ri

	// Assign levels to items.
	for _, item := range itemsData.Items {
		offset := item.StartOffset
		if offset >= len(text) {
			if nRunes > 0 {
				item.BidiLevel = paraLevels[nRunes-1]
				item.ParagraphLevel = paraLevels[nRunes-1]
			}
			continue
		}
		runeIdx := runeAtByte[offset]
		if runeIdx < nRunes {
			item.BidiLevel = allLevels[runeIdx]
			item.ParagraphLevel = paraLevels[runeIdx]
		}
	}
}

// resolveAllLevels applies W1-W7, N1-N2, and I1-I2 to compute final per-rune
// levels. Per UAX#9 BD13, level runs connected by isolate initiator/PDI pairs
// are joined into isolating run sequences, and W/N rules are applied to each
// sequence as a unit. This is critical for correct neutral resolution when
// neutrals span across isolation boundaries.
func resolveAllLevels(classes []xbidi.Class, embLevels []int, paraLevel int) []int {
	n := len(classes)
	if n == 0 {
		return nil
	}

	// Work on a copy of classes so we can mutate during resolution.
	types := make([]xbidi.Class, n)
	copy(types, classes)

	// Step 1: Split into level runs (contiguous spans at the same embedding level).
	type levelRun struct {
		start, end int
		level      int
	}
	var runs []levelRun
	i := 0
	for i < n {
		lvl := embLevels[i]
		start := i
		for i < n && embLevels[i] == lvl {
			i++
		}
		runs = append(runs, levelRun{start, i, lvl})
	}

	// Step 2: Build isolating run sequences (BD13).
	// Each sequence is a list of run indices. Level runs connected by an
	// isolate initiator (last char) → matching PDI (first char of next run
	// at the same level) are joined.
	//
	// Track which runs have been assigned to a sequence.
	assigned := make([]bool, len(runs))
	// Map: position of isolate initiator → index of run starting with matching PDI.
	matchingPDIRun := make(map[int]int) // position → run index

	// Find matching PDI for each isolate initiator using a stack.
	var isoStack []int // stack of initiator positions
	for pos := 0; pos < n; pos++ {
		cls := classes[pos]
		if cls == xbidi.LRI || cls == xbidi.RLI || cls == xbidi.FSI {
			isoStack = append(isoStack, pos)
		} else if cls == xbidi.PDI && len(isoStack) > 0 {
			initPos := isoStack[len(isoStack)-1]
			isoStack = isoStack[:len(isoStack)-1]
			// Find which run starts with this PDI.
			for ri, r := range runs {
				if r.start == pos {
					matchingPDIRun[initPos] = ri
					break
				}
			}
		}
	}

	// Build sequences by following isolate chains.
	type isoRunSeq struct {
		runIndices []int
	}
	var sequences []isoRunSeq

	for ri, r := range runs {
		if assigned[ri] {
			continue
		}
		seq := isoRunSeq{runIndices: []int{ri}}
		assigned[ri] = true

		// Follow the chain: if the last char of the current run is an
		// isolate initiator with a matching PDI run at the same level,
		// extend the sequence.
		curRun := r
		for {
			lastPos := curRun.end - 1
			lastCls := classes[lastPos]
			if lastCls != xbidi.LRI && lastCls != xbidi.RLI && lastCls != xbidi.FSI {
				break
			}
			pdiRI, ok := matchingPDIRun[lastPos]
			if !ok || assigned[pdiRI] || runs[pdiRI].level != curRun.level {
				break
			}
			seq.runIndices = append(seq.runIndices, pdiRI)
			assigned[pdiRI] = true
			curRun = runs[pdiRI]
		}

		sequences = append(sequences, seq)
	}

	// Step 3: For each isolating run sequence, apply W and N rules.
	for _, seq := range sequences {
		// Collect indices of all characters in this sequence.
		var indices []int
		for _, ri := range seq.runIndices {
			r := runs[ri]
			for j := r.start; j < r.end; j++ {
				indices = append(indices, j)
			}
		}
		if len(indices) == 0 {
			continue
		}

		// Extract types for processing.
		seqTypes := make([]xbidi.Class, len(indices))
		for k, idx := range indices {
			seqTypes[k] = types[idx]
		}

		// Compute sos and eos for this sequence.
		firstRun := runs[seq.runIndices[0]]
		lastRun := runs[seq.runIndices[len(seq.runIndices)-1]]
		lvl := firstRun.level

		prevLevel := paraLevel
		if firstRun.start > 0 {
			prevLevel = embLevels[firstRun.start-1]
		}
		succLevel := paraLevel
		if lastRun.end < n {
			succLevel = embLevels[lastRun.end]
		}
		sos := typeForLvl(maxInt(prevLevel, lvl))
		eos := typeForLvl(maxInt(succLevel, lvl))

		// Apply W and N rules to the concatenated sequence.
		resolveWeakTypes(seqTypes, sos)
		resolveNeutralTypes(seqTypes, lvl, sos, eos)

		// Write resolved types back.
		for k, idx := range indices {
			types[idx] = seqTypes[k]
		}
	}

	// Step 4: Apply implicit levels (I1, I2).
	levels := make([]int, n)
	for j := range levels {
		emb := embLevels[j]
		t := types[j]
		if emb%2 == 0 { // even (LTR) embedding
			switch t {
			case xbidi.R:
				levels[j] = emb + 1
			case xbidi.AN, xbidi.EN:
				levels[j] = emb + 2
			default: // L
				levels[j] = emb
			}
		} else { // odd (RTL) embedding
			if t == xbidi.R {
				levels[j] = emb
			} else { // L, AN, EN
				levels[j] = emb + 1
			}
		}
	}

	return levels
}

// resolveWeakTypes applies UAX#9 rules W1-W7 to a run of characters.
func resolveWeakTypes(types []xbidi.Class, sos xbidi.Class) {
	n := len(types)

	// W1: NSM → type of preceding character (or sos).
	prev := sos
	for i, t := range types {
		if t == xbidi.NSM {
			types[i] = prev
		}
		prev = types[i]
	}

	// W2: EN → AN when preceded by AL.
	lastStrong := sos
	for i, t := range types {
		if t == xbidi.EN && lastStrong == xbidi.AL {
			types[i] = xbidi.AN
		}
		if t == xbidi.L || t == xbidi.R || t == xbidi.AL {
			lastStrong = t
		}
	}

	// W3: AL → R.
	for i, t := range types {
		if t == xbidi.AL {
			types[i] = xbidi.R
		}
	}

	// W4: ES between two ENs → EN; CS between matching number types → that type.
	for i := 1; i < n-1; i++ {
		t := types[i]
		prev := types[i-1]
		next := types[i+1]
		if t == xbidi.ES && prev == xbidi.EN && next == xbidi.EN {
			types[i] = xbidi.EN
		}
		if t == xbidi.CS {
			if prev == xbidi.EN && next == xbidi.EN {
				types[i] = xbidi.EN
			} else if prev == xbidi.AN && next == xbidi.AN {
				types[i] = xbidi.AN
			}
		}
	}

	// W5: ET adjacent to EN → EN.
	// First pass: propagate EN forward through ETs.
	for i := 0; i < n; i++ {
		if types[i] == xbidi.ET {
			// Check if preceded by EN
			if i > 0 && types[i-1] == xbidi.EN {
				types[i] = xbidi.EN
			}
		}
	}
	// Second pass: propagate EN backward through ETs.
	for i := n - 1; i >= 0; i-- {
		if types[i] == xbidi.ET {
			if i < n-1 && types[i+1] == xbidi.EN {
				types[i] = xbidi.EN
			}
		}
	}

	// W6: Remaining ES, ET, CS → ON.
	for i, t := range types {
		if t == xbidi.ES || t == xbidi.ET || t == xbidi.CS {
			types[i] = xbidi.ON
		}
	}

	// W7: EN → L when the last strong type (from sos) is L.
	lastStrong = sos
	for i, t := range types {
		if t == xbidi.EN && lastStrong == xbidi.L {
			types[i] = xbidi.L
		}
		if t == xbidi.L || t == xbidi.R {
			lastStrong = t
		}
	}
}

// resolveNeutralTypes applies UAX#9 rules N1-N2 to a run of characters.
func resolveNeutralTypes(types []xbidi.Class, level int, sos, eos xbidi.Class) {
	n := len(types)

	isNeutral := func(c xbidi.Class) bool {
		switch c {
		case xbidi.WS, xbidi.ON, xbidi.B, xbidi.S,
			xbidi.RLI, xbidi.LRI, xbidi.FSI, xbidi.PDI, xbidi.BN:
			return true
		}
		return false
	}

	// Treat EN and AN as R for purposes of N1/N2 surrounding context.
	strongDir := func(c xbidi.Class) xbidi.Class {
		if c == xbidi.EN || c == xbidi.AN {
			return xbidi.R
		}
		return c
	}

	embDir := typeForLvl(level)

	for i := 0; i < n; {
		if !isNeutral(types[i]) {
			i++
			continue
		}

		// Start of neutral run.
		start := i
		for i < n && isNeutral(types[i]) {
			i++
		}
		end := i

		// N1/N2: determine lead and trail strong types.
		var lead, trail xbidi.Class

		// Look backward for preceding strong type.
		lead = sos
		for j := start - 1; j >= 0; j-- {
			t := types[j]
			if t == xbidi.L || t == xbidi.R || t == xbidi.EN || t == xbidi.AN {
				lead = strongDir(t)
				break
			}
		}

		// Look forward for following strong type.
		trail = eos
		for j := end; j < n; j++ {
			t := types[j]
			if t == xbidi.L || t == xbidi.R || t == xbidi.EN || t == xbidi.AN {
				trail = strongDir(t)
				break
			}
		}

		var resolved xbidi.Class
		if lead == trail {
			// N1: same direction on both sides.
			resolved = lead
		} else {
			// N2: different directions → embedding direction.
			resolved = embDir
		}

		for j := start; j < end; j++ {
			types[j] = resolved
		}
	}
}

// typeForLvl returns L for even levels, R for odd levels.
func typeForLvl(level int) xbidi.Class {
	if level%2 == 0 {
		return xbidi.L
	}
	return xbidi.R
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// applyL1 implements UAX#9 Rule L1: reset embedding levels of trailing
// whitespace (WS, FSI, LRI, RLI, PDI) and segment/paragraph separators
// to the paragraph embedding level. This is critical for correct L2
// reordering — without it, trailing spaces in RTL context participate in
// L2 reversals, changing the visual order of preceding content.
func applyL1(levels []int, runes []rune, paraLevel int) {
	n := len(runes)

	isL1Whitespace := func(r rune) bool {
		props, _ := xbidi.LookupRune(r)
		cls := props.Class()
		switch cls {
		case xbidi.WS, xbidi.FSI, xbidi.LRI, xbidi.RLI, xbidi.PDI, xbidi.BN:
			return true
		// Also include bidi control characters that are treated like WS for L1.
		case xbidi.LRE, xbidi.RLE, xbidi.LRO, xbidi.RLO, xbidi.PDF:
			return true
		}
		return false
	}

	// L1 clauses 1-3: Reset levels for B, S types and preceding whitespace.
	for i := 0; i < n; i++ {
		props, _ := xbidi.LookupRune(runes[i])
		cls := props.Class()
		if cls == xbidi.B || cls == xbidi.S {
			levels[i] = paraLevel
			// Reset preceding whitespace.
			for j := i - 1; j >= 0; j-- {
				if isL1Whitespace(runes[j]) {
					levels[j] = paraLevel
				} else {
					break
				}
			}
		}
	}

	// L1 clause 4: Reset trailing whitespace at end of paragraph.
	for i := n - 1; i >= 0; i-- {
		if isL1Whitespace(runes[i]) {
			levels[i] = paraLevel
		} else {
			break
		}
	}
}

// computeEmbeddingLevels tracks explicit bidi control characters in the text
// to compute per-rune embedding levels per UAX#9 rules X1-X8, X5a-X5c, X6a.
// It also applies rule X6: when an override is active, non-control characters
// have their bidi class forced to L (for LRO) or R (for RLO).
// The classes array is modified in-place for overridden characters.
func computeEmbeddingLevels(runes []rune, baseLevel int, classes []xbidi.Class) []int {
	levels := make([]int, len(runes))

	const maxDepth = 125

	// Override status: 0 = neutral, 1 = LTR (force L), 2 = RTL (force R)
	type dirEntry struct {
		level    int
		override int // 0=neutral, 1=LTR, 2=RTL
		isolate  bool
	}

	stack := make([]dirEntry, 1, 64)
	stack[0] = dirEntry{level: baseLevel}

	for i, r := range runes {
		cur := stack[len(stack)-1]

		switch r {
		case '\u202A': // LRE — next even level, neutral override
			levels[i] = cur.level
			newLevel := (cur.level | 1) + 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 0})
			}

		case '\u202B': // RLE — next odd level, neutral override
			levels[i] = cur.level
			newLevel := (cur.level + 1) | 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 0})
			}

		case '\u202D': // LRO — next even level, LTR override
			levels[i] = cur.level
			newLevel := (cur.level | 1) + 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 1})
			}

		case '\u202E': // RLO — next odd level, RTL override
			levels[i] = cur.level
			newLevel := (cur.level + 1) | 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, override: 2})
			}

		case '\u202C': // PDF — pop one non-isolate entry
			levels[i] = cur.level
			if len(stack) > 1 && !cur.isolate {
				stack = stack[:len(stack)-1]
			}

		case '\u2066': // LRI — next even level, isolate
			levels[i] = cur.level
			newLevel := (cur.level | 1) + 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, isolate: true})
			}

		case '\u2067': // RLI — next odd level, isolate
			levels[i] = cur.level
			newLevel := (cur.level + 1) | 1
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, isolate: true})
			}

		case '\u2068': // FSI
			levels[i] = cur.level
			dir := determineFSIDirection(runes[i+1:])
			var newLevel int
			if dir == 1 {
				newLevel = (cur.level + 1) | 1
			} else {
				newLevel = (cur.level | 1) + 1
			}
			if newLevel <= maxDepth && len(stack) < maxDepth+2 {
				stack = append(stack, dirEntry{level: newLevel, isolate: true})
			}

		case '\u2069': // PDI
			for len(stack) > 1 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.isolate {
					break
				}
			}
			levels[i] = stack[len(stack)-1].level

		default:
			levels[i] = cur.level
			// X6: apply override if active.
			if cur.override == 1 {
				classes[i] = xbidi.L
			} else if cur.override == 2 {
				classes[i] = xbidi.R
			}
		}
	}

	return levels
}

// determineFSIDirection implements UAX#9 P2/P3 for FSI.
func determineFSIDirection(runes []rune) int {
	isolateDepth := 0
	for _, r := range runes {
		switch r {
		case '\u2066', '\u2067', '\u2068':
			isolateDepth++
		case '\u2069':
			if isolateDepth > 0 {
				isolateDepth--
			} else {
				return 0
			}
		default:
			if isolateDepth == 0 {
				props, _ := xbidi.LookupRune(r)
				cls := props.Class()
				if cls == xbidi.L {
					return 0
				}
				if cls == xbidi.R || cls == xbidi.AL {
					return 1
				}
			}
		}
	}
	return 0
}

// isBidiControlChar returns true for Unicode bidi formatting characters.
func isBidiControlChar(r rune) bool {
	return (r >= '\u202A' && r <= '\u202E') || // LRE, RLE, PDF, LRO, RLO
		(r >= '\u2066' && r <= '\u2069') // LRI, RLI, FSI, PDI
}

// StripBidiControls removes bidi control characters from TextContent,
// remaps all item byte offsets, and strips RuneLevels entries.
func StripBidiControls(itemsData *InlineItemsData) {
	text := itemsData.TextContent
	if len(text) == 0 {
		return
	}

	hasBidiControls := false
	for _, r := range text {
		if isBidiControlChar(r) {
			hasBidiControls = true
			break
		}
	}
	if !hasBidiControls {
		return
	}

	offsetMap := make([]int, len(text)+1)
	var stripped strings.Builder
	stripped.Grow(len(text))
	var strippedLevels []int
	if itemsData.RuneLevels != nil {
		strippedLevels = make([]int, 0, len(itemsData.RuneLevels))
	}
	var strippedParaLevels []int
	if itemsData.ParagraphLevels != nil {
		strippedParaLevels = make([]int, 0, len(itemsData.ParagraphLevels))
	}
	newOff := 0
	runeIdx := 0

	for oldOff, r := range text {
		offsetMap[oldOff] = newOff
		if !isBidiControlChar(r) {
			stripped.WriteRune(r)
			newOff += utf8.RuneLen(r)
			if itemsData.RuneLevels != nil && runeIdx < len(itemsData.RuneLevels) {
				strippedLevels = append(strippedLevels, itemsData.RuneLevels[runeIdx])
			}
			if itemsData.ParagraphLevels != nil && runeIdx < len(itemsData.ParagraphLevels) {
				strippedParaLevels = append(strippedParaLevels, itemsData.ParagraphLevels[runeIdx])
			}
		}
		runeIdx++
	}
	offsetMap[len(text)] = newOff

	itemsData.TextContent = stripped.String()
	itemsData.RuneLevels = strippedLevels
	itemsData.ParagraphLevels = strippedParaLevels

	for _, item := range itemsData.Items {
		item.StartOffset = offsetMap[item.StartOffset]
		item.EndOffset = offsetMap[item.EndOffset]
	}
}

// SplitItemsAtLevelBoundaries splits text items where bidi levels change.
func SplitItemsAtLevelBoundaries(itemsData *InlineItemsData) {
	if len(itemsData.RuneLevels) == 0 {
		return
	}

	text := itemsData.TextContent

	nRunes := utf8.RuneCountInString(text)
	runeAtByte := make([]int, len(text)+1)
	byteAtRune := make([]int, nRunes+1)
	ri := 0
	for bi := range text {
		runeAtByte[bi] = ri
		byteAtRune[ri] = bi
		ri++
	}
	runeAtByte[len(text)] = ri
	byteAtRune[ri] = len(text)

	var newItems []*InlineItem

	for _, item := range itemsData.Items {
		if item.Type != InlineItemText {
			// Non-text items: update level from RuneLevels if within text,
			// otherwise keep the level already assigned by ResolveBidiLevels
			// (which uses the paragraph embedding level for end-of-text items).
			if item.StartOffset < len(text) {
				rIdx := runeAtByte[item.StartOffset]
				if item.Type == InlineItemCloseTag && rIdx > 0 {
					// CloseTag: use the level of the rune BEFORE the offset
					// (the last rune of the content being closed). This ensures
					// the close tag stays with its content during L2 reordering,
					// matching Blink's behavior where inline boundaries move
					// with their content in bidi reordering.
					if rIdx-1 < len(itemsData.RuneLevels) {
						item.BidiLevel = itemsData.RuneLevels[rIdx-1]
					}
				} else if rIdx < len(itemsData.RuneLevels) {
					item.BidiLevel = itemsData.RuneLevels[rIdx]
				}
			}
			// Items past end of text keep their BidiLevel from ResolveBidiLevels.
			newItems = append(newItems, item)
			continue
		}

		startRune := runeAtByte[item.StartOffset]
		endRune := runeAtByte[item.EndOffset]

		if startRune >= endRune || startRune >= len(itemsData.RuneLevels) {
			item.BidiLevel = 0
			if startRune < len(itemsData.RuneLevels) {
				item.BidiLevel = itemsData.RuneLevels[startRune]
			}
			newItems = append(newItems, item)
			continue
		}

		currentLevel := itemsData.RuneLevels[startRune]
		runStart := startRune

		for r := startRune + 1; r < endRune && r < len(itemsData.RuneLevels); r++ {
			lvl := itemsData.RuneLevels[r]
			if lvl != currentLevel {
				newItems = append(newItems, &InlineItem{
					Type:            InlineItemText,
					StartOffset:     byteAtRune[runStart],
					EndOffset:       byteAtRune[r],
					Node:            item.Node,
					Style:           item.Style,
					BidiLevel:       currentLevel,
					ParagraphLevel:  item.ParagraphLevel,
					IsFirstFragment: item.IsFirstFragment,
					IsLastFragment:  item.IsLastFragment,

					EnclosingPaintGroup: item.EnclosingPaintGroup,
				})
				currentLevel = lvl
				runStart = r
			}
		}

		newItems = append(newItems, &InlineItem{
			Type:            InlineItemText,
			StartOffset:     byteAtRune[runStart],
			EndOffset:       byteAtRune[endRune],
			Node:            item.Node,
			Style:           item.Style,
			BidiLevel:       currentLevel,
			ParagraphLevel:  item.ParagraphLevel,
			IsFirstFragment: item.IsFirstFragment,
			IsLastFragment:  item.IsLastFragment,

			EnclosingPaintGroup: item.EnclosingPaintGroup,
		})
	}

	itemsData.Items = newItems
}

// ReorderLineVisual reorders a line's InlineItemResults from logical order
// to visual order using the UAX#9 L2 algorithm. paragraphLevel is the
// paragraph embedding level (0 for LTR, 1 for RTL).
//
// Following ICU's ubidi_reorderVisual, when the paragraph level is odd
// (RTL), minOdd is forced to 1 so that L2 reverses at the paragraph level
// even when all items have even embedding levels (e.g., LRO-overridden
// text in an RTL paragraph).
func ReorderLineVisual(results []InlineItemResult, paragraphLevel int) {
	if len(results) == 0 {
		return
	}

	maxLevel := 0
	minOdd := 256
	for _, r := range results {
		lvl := r.Item.BidiLevel
		if lvl > maxLevel {
			maxLevel = lvl
		}
		if lvl%2 == 1 && lvl < minOdd {
			minOdd = lvl
		}
	}

	// ICU's approach: force minOdd to include the paragraph level.
	// This ensures RTL paragraphs get reversed even when all items
	// are at even levels (e.g., LRO override in RTL context).
	if paragraphLevel%2 == 1 && paragraphLevel < minOdd {
		minOdd = paragraphLevel
	}

	if maxLevel == 0 || minOdd == 256 {
		return
	}

	for lvl := maxLevel; lvl >= minOdd; lvl-- {
		for i := 0; i < len(results); {
			if results[i].Item.BidiLevel >= lvl {
				j := i + 1
				for j < len(results) && results[j].Item.BidiLevel >= lvl {
					j++
				}
				for a, b := i, j-1; a < b; a, b = a+1, b-1 {
					results[a], results[b] = results[b], results[a]
				}
				i = j
			} else {
				i++
			}
		}
	}
}
