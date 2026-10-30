package text

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/andybalholm/brotli"
)

// WOFF2 file format: https://www.w3.org/TR/WOFF2/
//
// convertWOFF2ToTTF is a Go port of google/woff2's ConvertWOFF2ToTTF
// (src/woff2_dec.cc @ 7b67e12355fe7da148db5c19d4261f0a498a9537), the decoder
// Blink calls from DecodeFontWithIft's kWoff2Tag branch
// (platform/fonts/web_font_decoder.cc:189 @
// 5992657f556bcd9d207a4aca187488462f027cab). Function names mirror woff2_dec.cc.
// Font collections (flavor 'ttcf') are not supported.

const (
	woff2Signature   = 0x774F4632 // 'wOF2'
	ttcFontFlavor    = 0x74746366 // 'ttcf'
	sfntHeaderSize   = 12
	sfntEntrySize    = 16
	tagGlyf          = 0x676c7966
	tagLoca          = 0x6c6f6361
	tagHead          = 0x68656164
	tagHhea          = 0x68686561
	tagHmtx          = 0x686d7478
	maxWOFF2SFNTSize = 30 << 20 // Blink's kMaxDecompressedSize (30MB)
)

// Glyph flags (TrueType glyf and composite glyph descriptions).
const (
	glyfOnCurve       = 1 << 0
	glyfXShort        = 1 << 1
	glyfYShort        = 1 << 2
	glyfRepeat        = 1 << 3
	glyfThisXIsSame   = 1 << 4
	glyfThisYIsSame   = 1 << 5
	glyfOverlapSimple = 1 << 6

	flagArg1And2AreWords    = 1 << 0
	flagWeHaveAScale        = 1 << 3
	flagMoreComponents      = 1 << 5
	flagWeHaveAnXAndYScale  = 1 << 6
	flagWeHaveATwoByTwo     = 1 << 7
	flagWeHaveInstructions  = 1 << 8
	flagOverlapSimpleBitmap = 1 << 0 // transformed glyf optionFlags

)

// woff2KnownTags is woff2's kKnownTags (src/table_tags.cc), indexed by the
// low six bits of a table directory entry's flag byte.
var woff2KnownTags = [63]string{
	"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post", "cvt ",
	"fpgm", "glyf", "loca", "prep", "CFF ", "VORG", "EBDT", "EBLC", "gasp",
	"hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea", "vmtx", "BASE", "GDEF",
	"GPOS", "GSUB", "EBSC", "JSTF", "MATH", "CBDT", "CBLC", "COLR", "CPAL",
	"SVG ", "sbix", "acnt", "avar", "bdat", "bloc", "bsln", "cvar", "fdsc",
	"feat", "fmtx", "fvar", "gvar", "hsty", "just", "lcar", "mort", "morx",
	"opbd", "prop", "trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
}

var errWOFF2 = errors.New("woff2: malformed font")

// isWOFF2 checks if data starts with the WOFF2 signature.
func isWOFF2(data []byte) bool {
	return len(data) >= 4 && binary.BigEndian.Uint32(data) == woff2Signature
}

// decodeWebFont turns fetched @font-face data into SFNT bytes, choosing the
// decoder by the data's signature rather than the declared format(), as
// Blink's DecodeFontWithIft does (platform/fonts/web_font_decoder.cc:189 @
// 5992657f556bcd9d207a4aca187488462f027cab). Raw SFNT data passes through.
func decodeWebFont(data []byte) ([]byte, error) {
	switch {
	case isWOFF2(data):
		sfnt, err := convertWOFF2ToTTF(data)
		if err != nil {
			return nil, fmt.Errorf("converting WOFF2: %w", err)
		}
		return sfnt, nil
	case isWOFF1(data):
		sfnt, err := decompressWOFF1(data)
		if err != nil {
			return nil, fmt.Errorf("decompressing WOFF1: %w", err)
		}
		return sfnt, nil
	}
	return data, nil
}

// woff2Buffer is woff2's Buffer: a bounds-checked big-endian reader.
type woff2Buffer struct {
	data []byte
	off  int
}

func (b *woff2Buffer) remaining() int { return len(b.data) - b.off }

func (b *woff2Buffer) skip(n int) bool {
	if n < 0 || n > b.remaining() {
		return false
	}
	b.off += n
	return true
}

func (b *woff2Buffer) read(n int) ([]byte, bool) {
	if n < 0 || n > b.remaining() {
		return nil, false
	}
	p := b.data[b.off : b.off+n]
	b.off += n
	return p, true
}

func (b *woff2Buffer) readU8() (uint8, bool) {
	p, ok := b.read(1)
	if !ok {
		return 0, false
	}
	return p[0], true
}

func (b *woff2Buffer) readU16() (uint16, bool) {
	p, ok := b.read(2)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint16(p), true
}

func (b *woff2Buffer) readU32() (uint32, bool) {
	p, ok := b.read(4)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint32(p), true
}

// read255UShort mirrors woff2 Read255UShort (src/variable_length.cc).
func read255UShort(b *woff2Buffer) (int, bool) {
	const wordCode, oneMoreByteCode2, oneMoreByteCode1, lowestUCode = 253, 254, 255, 253
	code, ok := b.readU8()
	if !ok {
		return 0, false
	}
	switch code {
	case wordCode:
		v, ok := b.readU16()
		return int(v), ok
	case oneMoreByteCode1:
		v, ok := b.readU8()
		return int(v) + lowestUCode, ok
	case oneMoreByteCode2:
		v, ok := b.readU8()
		return int(v) + lowestUCode*2, ok
	}
	return int(code), true
}

// readBase128 mirrors woff2 ReadBase128 (src/variable_length.cc).
func readBase128(b *woff2Buffer) (uint32, bool) {
	var result uint32
	for i := 0; i < 5; i++ {
		code, ok := b.readU8()
		if !ok {
			return 0, false
		}
		// Leading zeros are invalid.
		if i == 0 && code == 0x80 {
			return 0, false
		}
		// If any of the top seven bits are set then we're about to overflow.
		if result&0xfe000000 != 0 {
			return 0, false
		}
		result = result<<7 | uint32(code&0x7f)
		if code&0x80 == 0 {
			return result, true
		}
	}
	// Make sure not to exceed the size bound.
	return 0, false
}

// woff2Table is one table directory entry (woff2's Table).
type woff2Table struct {
	tag             uint32
	transformed     bool
	srcOffset       int // offset in the decompressed stream
	transformLength int // length in the decompressed stream
	origLength      int // dst_length: length of the reconstructed table
}

// readTableDirectory mirrors woff2 ReadTableDirectory.
func readTableDirectory(b *woff2Buffer, numTables int) ([]woff2Table, bool) {
	tables := make([]woff2Table, numTables)
	srcOffset := 0
	for i := range tables {
		flagByte, ok := b.readU8()
		if !ok {
			return nil, false
		}
		var tag uint32
		if flagByte&0x3f == 0x3f {
			if tag, ok = b.readU32(); !ok {
				return nil, false
			}
		} else {
			tag = binary.BigEndian.Uint32([]byte(woff2KnownTags[flagByte&0x3f]))
		}
		// Transform version 0 means "transformed" for glyf/loca and "null
		// transform" for every other table.
		xformVersion := (flagByte >> 6) & 0x03
		transformed := xformVersion != 0
		if tag == tagGlyf || tag == tagLoca {
			transformed = xformVersion == 0
		}
		origLength, ok := readBase128(b)
		if !ok {
			return nil, false
		}
		transformLength := origLength
		if transformed {
			if transformLength, ok = readBase128(b); !ok {
				return nil, false
			}
			if tag == tagLoca && transformLength != 0 {
				return nil, false
			}
		}
		tables[i] = woff2Table{
			tag:             tag,
			transformed:     transformed,
			srcOffset:       srcOffset,
			transformLength: int(transformLength),
			origLength:      int(origLength),
		}
		srcOffset += int(transformLength)
	}
	return tables, true
}

// woff2Header is the subset of woff2's WOFF2Header the decoder needs.
type woff2Header struct {
	flavor           uint32
	tables           []woff2Table
	compressedOffset int
	compressedLength int
	uncompressedSize int
}

// readWOFF2Header mirrors woff2 ReadWOFF2Header for single fonts.
func readWOFF2Header(data []byte) (*woff2Header, error) {
	b := &woff2Buffer{data: data}
	signature, ok := b.readU32()
	if !ok || signature != woff2Signature {
		return nil, errWOFF2
	}
	hdr := &woff2Header{}
	if hdr.flavor, ok = b.readU32(); !ok {
		return nil, errWOFF2
	}
	if hdr.flavor == ttcFontFlavor {
		return nil, fmt.Errorf("woff2: font collections are not supported")
	}
	if reported, ok := b.readU32(); !ok || int(reported) != len(data) {
		return nil, errWOFF2
	}
	numTables, ok := b.readU16()
	if !ok || numTables == 0 {
		return nil, errWOFF2
	}
	// reserved (2), totalSfntSize (4)
	if !b.skip(6) {
		return nil, errWOFF2
	}
	compressedLength, ok := b.readU32()
	if !ok {
		return nil, errWOFF2
	}
	hdr.compressedLength = int(compressedLength)
	// majorVersion, minorVersion, metaOffset/Length/OrigLength, privOffset/Length
	if !b.skip(2*2 + 5*4) {
		return nil, errWOFF2
	}
	if hdr.tables, ok = readTableDirectory(b, int(numTables)); !ok {
		return nil, errWOFF2
	}
	last := hdr.tables[len(hdr.tables)-1]
	hdr.uncompressedSize = last.srcOffset + last.transformLength
	hdr.compressedOffset = b.off
	if hdr.compressedOffset+hdr.compressedLength > len(data) {
		return nil, errWOFF2
	}
	return hdr, nil
}

// woff2Uncompress mirrors woff2 Woff2Uncompress: the table data is a single
// Brotli stream that must decompress to exactly dstSize bytes.
func woff2Uncompress(src []byte, dstSize int) ([]byte, error) {
	dst := make([]byte, dstSize)
	r := brotli.NewReader(bytes.NewReader(src))
	if _, err := io.ReadFull(r, dst); err != nil {
		return nil, fmt.Errorf("woff2: brotli: %w", err)
	}
	if n, _ := r.Read(make([]byte, 1)); n != 0 {
		return nil, fmt.Errorf("woff2: brotli stream longer than declared")
	}
	return dst, nil
}

// woff2Point is woff2's Point.
type woff2Point struct {
	x, y    int
	onCurve bool
}

func withSign(flag, baseval int) int {
	if flag&1 != 0 {
		return baseval
	}
	return -baseval
}

// tripletDecode mirrors woff2 TripletDecode: it decodes nPoints (flag,
// triplet) pairs into absolute coordinates and returns the triplet bytes used.
func tripletDecode(flagsIn, in []byte, points []woff2Point) (int, bool) {
	x, y := 0, 0
	if len(points) > len(in) {
		return 0, false
	}
	tripletIndex := 0
	for i := range points {
		flag := int(flagsIn[i])
		onCurve := flag>>7 == 0
		flag &= 0x7f
		var nDataBytes int
		switch {
		case flag < 84:
			nDataBytes = 1
		case flag < 120:
			nDataBytes = 2
		case flag < 124:
			nDataBytes = 3
		default:
			nDataBytes = 4
		}
		if tripletIndex+nDataBytes > len(in) {
			return 0, false
		}
		t := in[tripletIndex:]
		var dx, dy int
		switch {
		case flag < 10:
			dx = 0
			dy = withSign(flag, (flag&14)<<7+int(t[0]))
		case flag < 20:
			dx = withSign(flag, ((flag-10)&14)<<7+int(t[0]))
			dy = 0
		case flag < 84:
			b0 := flag - 20
			b1 := int(t[0])
			dx = withSign(flag, 1+(b0&0x30)+(b1>>4))
			dy = withSign(flag>>1, 1+((b0&0x0c)<<2)+(b1&0x0f))
		case flag < 120:
			b0 := flag - 84
			dx = withSign(flag, 1+((b0/12)<<8)+int(t[0]))
			dy = withSign(flag>>1, 1+(((b0%12)>>2)<<8)+int(t[1]))
		case flag < 124:
			b2 := int(t[1])
			dx = withSign(flag, int(t[0])<<4+b2>>4)
			dy = withSign(flag>>1, (b2&0x0f)<<8+int(t[2]))
		default:
			dx = withSign(flag, int(t[0])<<8+int(t[1]))
			dy = withSign(flag>>1, int(t[2])<<8+int(t[3]))
		}
		tripletIndex += nDataBytes
		x += dx
		y += dy
		points[i] = woff2Point{x: x, y: y, onCurve: onCurve}
	}
	return tripletIndex, true
}

func appendU16(dst []byte, v int) []byte {
	return binary.BigEndian.AppendUint16(dst, uint16(v))
}

// storePoints mirrors woff2 StorePoints: it appends the TrueType flags (with
// repeat runs) and the x / y coordinate arrays for a simple glyph.
func storePoints(dst []byte, points []woff2Point, hasOverlapBit bool) []byte {
	var flags, xs, ys []byte
	lastFlag, repeatCount := -1, 0
	lastX, lastY := 0, 0
	for i, p := range points {
		flag := 0
		if p.onCurve {
			flag = glyfOnCurve
		}
		if hasOverlapBit && i == 0 {
			flag |= glyfOverlapSimple
		}
		dx, dy := p.x-lastX, p.y-lastY
		switch {
		case dx == 0:
			flag |= glyfThisXIsSame
		case dx > -256 && dx < 256:
			flag |= glyfXShort
			if dx > 0 {
				flag |= glyfThisXIsSame
			}
			xs = append(xs, byte(abs(dx)))
		default:
			xs = appendU16(xs, dx)
		}
		switch {
		case dy == 0:
			flag |= glyfThisYIsSame
		case dy > -256 && dy < 256:
			flag |= glyfYShort
			if dy > 0 {
				flag |= glyfThisYIsSame
			}
			ys = append(ys, byte(abs(dy)))
		default:
			ys = appendU16(ys, dy)
		}
		if flag == lastFlag && repeatCount != 255 {
			flags[len(flags)-1] |= glyfRepeat
			repeatCount++
		} else {
			if repeatCount != 0 {
				flags = append(flags, byte(repeatCount))
			}
			flags = append(flags, byte(flag))
			repeatCount = 0
		}
		lastX, lastY, lastFlag = p.x, p.y, flag
	}
	if repeatCount != 0 {
		flags = append(flags, byte(repeatCount))
	}
	dst = append(dst, flags...)
	dst = append(dst, xs...)
	return append(dst, ys...)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// computeBbox mirrors woff2 ComputeBbox: xMin, yMin, xMax, yMax of points.
func computeBbox(points []woff2Point) [4]int {
	if len(points) == 0 {
		return [4]int{}
	}
	xMin, yMin, xMax, yMax := points[0].x, points[0].y, points[0].x, points[0].y
	for _, p := range points[1:] {
		xMin, xMax = min(xMin, p.x), max(xMax, p.x)
		yMin, yMax = min(yMin, p.y), max(yMax, p.y)
	}
	return [4]int{xMin, yMin, xMax, yMax}
}

// sizeOfComposite mirrors woff2 SizeOfComposite: the byte length of the next
// composite glyph's component records, and whether it carries instructions.
func sizeOfComposite(composite woff2Buffer) (size int, haveInstructions bool, ok bool) {
	start := composite.off
	flags := uint16(flagMoreComponents)
	for flags&flagMoreComponents != 0 {
		if flags, ok = composite.readU16(); !ok {
			return 0, false, false
		}
		haveInstructions = haveInstructions || flags&flagWeHaveInstructions != 0
		argSize := 2 // glyph index
		if flags&flagArg1And2AreWords != 0 {
			argSize += 4
		} else {
			argSize += 2
		}
		switch {
		case flags&flagWeHaveAScale != 0:
			argSize += 2
		case flags&flagWeHaveAnXAndYScale != 0:
			argSize += 4
		case flags&flagWeHaveATwoByTwo != 0:
			argSize += 8
		}
		if !composite.skip(argSize) {
			return 0, false, false
		}
	}
	return composite.off - start, haveInstructions, true
}

// pad4 appends zero bytes up to a 4-byte boundary.
func pad4(b []byte) []byte {
	var zeros [3]byte
	return append(b, zeros[:(4-len(b)%4)%4]...)
}

// woff2FontInfo is woff2's WOFF2FontInfo.
type woff2FontInfo struct {
	numGlyphs   int
	indexFormat int
	numHMetrics int
	xMins       []int16
}

// reconstructGlyf mirrors woff2 ReconstructGlyf: it rebuilds the TrueType
// glyf table from the seven transformed sub-streams and returns it with the
// matching loca table.
func reconstructGlyf(data []byte, loca woff2Table, info *woff2FontInfo) (glyf, locaOut []byte, err error) {
	const numSubStreams = 7
	file := &woff2Buffer{data: data}
	_, _ = file.readU16() // version
	optionFlags, ok1 := file.readU16()
	numGlyphs, ok2 := file.readU16()
	indexFormat, ok3 := file.readU16()
	if !ok1 || !ok2 || !ok3 {
		return nil, nil, errWOFF2
	}
	hasOverlapBitmap := optionFlags&flagOverlapSimpleBitmap != 0
	info.numGlyphs, info.indexFormat = int(numGlyphs), int(indexFormat)

	// https://www.w3.org/TR/WOFF2/#conform-mustRejectLoca
	locaEntrySize := 2
	if indexFormat != 0 {
		locaEntrySize = 4
	}
	if loca.origLength != locaEntrySize*(info.numGlyphs+1) {
		return nil, nil, errWOFF2
	}

	offset := (2 + numSubStreams) * 4
	if offset > len(data) {
		return nil, nil, errWOFF2
	}
	var substreams [numSubStreams]woff2Buffer
	for i := range substreams {
		size, ok := file.readU32()
		if !ok || int(size) > len(data)-offset {
			return nil, nil, errWOFF2
		}
		substreams[i] = woff2Buffer{data: data[offset : offset+int(size)]}
		offset += int(size)
	}
	nContourStream, nPointsStream, flagStream, glyphStream := &substreams[0], &substreams[1], &substreams[2], &substreams[3]
	compositeStream, bboxStream, instructionStream := &substreams[4], &substreams[5], &substreams[6]

	var overlapBitmap []byte
	if hasOverlapBitmap {
		n := (info.numGlyphs + 7) >> 3
		if n > len(data)-offset {
			return nil, nil, errWOFF2
		}
		overlapBitmap = data[offset : offset+n]
	}

	bboxBitmap, ok := bboxStream.read(((info.numGlyphs + 31) >> 5) << 2)
	if !ok {
		return nil, nil, errWOFF2
	}

	locaValues := make([]int, info.numGlyphs+1)
	info.xMins = make([]int16, info.numGlyphs)
	var nPointsVec []int
	var points []woff2Point
	for i := 0; i < info.numGlyphs; i++ {
		var glyph []byte
		haveBbox := bboxBitmap[i>>3]&(0x80>>(i&7)) != 0
		nContours, ok := nContourStream.readU16()
		if !ok {
			return nil, nil, errWOFF2
		}

		switch {
		case nContours == 0xffff:
			// Composite glyph; must have an explicit bbox.
			if !haveBbox {
				return nil, nil, errWOFF2
			}
			compositeSize, haveInstructions, ok := sizeOfComposite(*compositeStream)
			if !ok {
				return nil, nil, errWOFF2
			}
			instructionSize := 0
			if haveInstructions {
				if instructionSize, ok = read255UShort(glyphStream); !ok {
					return nil, nil, errWOFF2
				}
			}
			glyph = appendU16(glyph, int(nContours))
			bbox, ok1 := bboxStream.read(8)
			components, ok2 := compositeStream.read(compositeSize)
			if !ok1 || !ok2 {
				return nil, nil, errWOFF2
			}
			glyph = append(append(glyph, bbox...), components...)
			if haveInstructions {
				instructions, ok := instructionStream.read(instructionSize)
				if !ok {
					return nil, nil, errWOFF2
				}
				glyph = append(appendU16(glyph, instructionSize), instructions...)
			}

		case nContours > 0:
			// Simple glyph.
			nPointsVec = nPointsVec[:0]
			totalNPoints := 0
			for j := 0; j < int(nContours); j++ {
				n, ok := read255UShort(nPointsStream)
				if !ok {
					return nil, nil, errWOFF2
				}
				nPointsVec = append(nPointsVec, n)
				totalNPoints += n
			}
			flagsBuf, ok := flagStream.read(totalNPoints)
			if !ok {
				return nil, nil, errWOFF2
			}
			if cap(points) < totalNPoints {
				points = make([]woff2Point, totalNPoints)
			}
			points = points[:totalNPoints]
			consumed, ok := tripletDecode(flagsBuf, glyphStream.data[glyphStream.off:], points)
			if !ok || !glyphStream.skip(consumed) {
				return nil, nil, errWOFF2
			}
			instructionSize, ok := read255UShort(glyphStream)
			if !ok {
				return nil, nil, errWOFF2
			}

			glyph = appendU16(glyph, int(nContours))
			if haveBbox {
				bbox, ok := bboxStream.read(8)
				if !ok {
					return nil, nil, errWOFF2
				}
				glyph = append(glyph, bbox...)
			} else {
				for _, v := range computeBbox(points) {
					glyph = appendU16(glyph, v)
				}
			}
			endPoint := -1
			for _, n := range nPointsVec {
				endPoint += n
				if endPoint >= 65536 {
					return nil, nil, errWOFF2
				}
				glyph = appendU16(glyph, endPoint)
			}
			instructions, ok := instructionStream.read(instructionSize)
			if !ok {
				return nil, nil, errWOFF2
			}
			glyph = append(appendU16(glyph, instructionSize), instructions...)
			hasOverlapBit := hasOverlapBitmap && overlapBitmap[i>>3]&(0x80>>(i&7)) != 0
			glyph = storePoints(glyph, points, hasOverlapBit)

		default:
			// Empty glyph; must not have a bbox.
			if haveBbox {
				return nil, nil, errWOFF2
			}
		}

		locaValues[i] = len(glyf)
		glyf = pad4(append(glyf, glyph...))

		// x_min is needed to reconstruct a transformed hmtx.
		if nContours > 0 && nContours != 0xffff {
			info.xMins[i] = int16(binary.BigEndian.Uint16(glyph[2:4]))
		}
	}
	// loca[n] equals the length of the glyf table.
	locaValues[info.numGlyphs] = len(glyf)
	return glyf, storeLoca(locaValues, info.indexFormat), nil
}

// storeLoca mirrors woff2 StoreLoca.
func storeLoca(locaValues []int, indexFormat int) []byte {
	var loca []byte
	for _, v := range locaValues {
		if indexFormat != 0 {
			loca = binary.BigEndian.AppendUint32(loca, uint32(v))
		} else {
			loca = appendU16(loca, v>>1)
		}
	}
	return loca
}

// reconstructTransformedHmtx mirrors woff2 ReconstructTransformedHmtx: left
// side bearings omitted by the encoder are recovered from the glyphs' xMin.
func reconstructTransformedHmtx(transformed []byte, info *woff2FontInfo) ([]byte, error) {
	in := &woff2Buffer{data: transformed}
	hmtxFlags, ok := in.readU8()
	if !ok {
		return nil, errWOFF2
	}
	hasProportionalLsbs := hmtxFlags&1 == 0
	hasMonospaceLsbs := hmtxFlags&2 == 0
	// Bits 2-7 are reserved and MUST be zero; a transform must drop something.
	if hmtxFlags&0xfc != 0 || (hasProportionalLsbs && hasMonospaceLsbs) {
		return nil, errWOFF2
	}
	numGlyphs, numHMetrics := info.numGlyphs, info.numHMetrics
	if numHMetrics > numGlyphs || numHMetrics < 1 || len(info.xMins) != numGlyphs {
		return nil, errWOFF2
	}
	advanceWidths := make([]uint16, numHMetrics)
	for i := range advanceWidths {
		if advanceWidths[i], ok = in.readU16(); !ok {
			return nil, errWOFF2
		}
	}
	lsbs := make([]int16, numGlyphs)
	for i := range lsbs {
		hasLsb := hasMonospaceLsbs
		if i < numHMetrics {
			hasLsb = hasProportionalLsbs
		}
		if !hasLsb {
			lsbs[i] = info.xMins[i]
			continue
		}
		v, ok := in.readU16()
		if !ok {
			return nil, errWOFF2
		}
		lsbs[i] = int16(v)
	}
	hmtx := make([]byte, 0, 2*numGlyphs+2*numHMetrics)
	for i := 0; i < numGlyphs; i++ {
		if i < numHMetrics {
			hmtx = binary.BigEndian.AppendUint16(hmtx, advanceWidths[i])
		}
		hmtx = appendU16(hmtx, int(lsbs[i]))
	}
	return hmtx, nil
}

// computeULongSum is the OpenType table checksum.
func computeULongSum(data []byte) uint32 {
	var sum uint32
	for i := 0; i < len(data); i += 4 {
		var w [4]byte
		copy(w[:], data[i:])
		sum += binary.BigEndian.Uint32(w[:])
	}
	return sum
}

// convertWOFF2ToTTF mirrors woff2 ConvertWOFF2ToTTF for single fonts: it
// decompresses the table data, reverses the glyf/loca and hmtx transforms
// (ReconstructFont) and writes an SFNT with tables in tag order and fresh
// checksums.
func convertWOFF2ToTTF(data []byte) ([]byte, error) {
	hdr, err := readWOFF2Header(data)
	if err != nil {
		return nil, err
	}
	if hdr.uncompressedSize > maxWOFF2SFNTSize {
		return nil, fmt.Errorf("woff2: decompressed size exceeds %d bytes", maxWOFF2SFNTSize)
	}
	transformed, err := woff2Uncompress(data[hdr.compressedOffset:hdr.compressedOffset+hdr.compressedLength], hdr.uncompressedSize)
	if err != nil {
		return nil, err
	}

	byTag := make(map[uint32]woff2Table, len(hdr.tables))
	for _, t := range hdr.tables {
		if _, dup := byTag[t.tag]; dup {
			return nil, errWOFF2
		}
		byTag[t.tag] = t
	}
	src := func(t woff2Table) []byte { return transformed[t.srcOffset : t.srcOffset+t.transformLength] }

	glyfT, hasGlyf := byTag[tagGlyf]
	locaT, hasLoca := byTag[tagLoca]
	if hasGlyf != hasLoca || (hasGlyf && glyfT.transformed != locaT.transformed) {
		return nil, errWOFF2
	}

	// ReconstructFont processes tables in directory order; hmtx depends on
	// hhea (numberOfHMetrics) and glyf (x_mins), so resolve those first.
	info := &woff2FontInfo{}
	if hhea, ok := byTag[tagHhea]; ok {
		// numberOfHMetrics is at offset 34 of hhea (ReadNumHMetrics).
		b := &woff2Buffer{data: src(hhea)}
		v, ok := uint16(0), b.skip(34)
		if ok {
			v, ok = b.readU16()
		}
		if !ok {
			return nil, errWOFF2
		}
		info.numHMetrics = int(v)
	}
	out := make(map[uint32][]byte, len(hdr.tables))
	if hasGlyf && glyfT.transformed {
		glyf, loca, err := reconstructGlyf(src(glyfT), locaT, info)
		if err != nil {
			return nil, err
		}
		out[tagGlyf], out[tagLoca] = glyf, loca
	}
	for _, t := range hdr.tables {
		if _, done := out[t.tag]; done {
			continue
		}
		switch {
		case !t.transformed:
			out[t.tag] = src(t)
		case t.tag == tagHmtx:
			hmtx, err := reconstructTransformedHmtx(src(t), info)
			if err != nil {
				return nil, err
			}
			out[t.tag] = hmtx
		default:
			return nil, fmt.Errorf("woff2: unknown transform for table %08x", t.tag)
		}
	}

	return storeSFNT(hdr.flavor, out)
}

// storeSFNT writes the offset table, the table records (sorted by tag) and
// the 4-byte padded table data, then sets head.checkSumAdjustment
// (StoreOffsetTable / StoreTableEntry / ReconstructFont in woff2_dec.cc).
func storeSFNT(flavor uint32, tables map[uint32][]byte) ([]byte, error) {
	tags := make([]uint32, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })

	numTables := len(tags)
	searchRange, entrySelector, rangeShift := sfntSearchParams(numTables)

	sfnt := binary.BigEndian.AppendUint32(nil, flavor)
	sfnt = appendU16(sfnt, numTables)
	sfnt = appendU16(sfnt, searchRange)
	sfnt = appendU16(sfnt, entrySelector)
	sfnt = appendU16(sfnt, rangeShift)

	offset := sfntHeaderSize + sfntEntrySize*numTables
	headOffset := -1
	var body []byte
	for _, tag := range tags {
		data := tables[tag]
		if tag == tagHead {
			if len(data) < 12 {
				return nil, errWOFF2
			}
			// checkSumAdjustment is computed over the font with this field zeroed.
			data = append([]byte(nil), data...)
			binary.BigEndian.PutUint32(data[8:], 0)
			headOffset = offset + len(body)
		}
		sfnt = binary.BigEndian.AppendUint32(sfnt, tag)
		sfnt = binary.BigEndian.AppendUint32(sfnt, computeULongSum(data))
		sfnt = binary.BigEndian.AppendUint32(sfnt, uint32(offset+len(body)))
		sfnt = binary.BigEndian.AppendUint32(sfnt, uint32(len(data)))
		body = pad4(append(body, data...))
	}
	sfnt = append(sfnt, body...)
	if headOffset >= 0 {
		binary.BigEndian.PutUint32(sfnt[headOffset+8:], 0xB1B0AFBA-computeULongSum(sfnt))
	}
	return sfnt, nil
}
