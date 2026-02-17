package text

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// WOFF1 file format: https://www.w3.org/TR/WOFF/

const woffSignature = 0x774F4646 // 'wOFF'

// isWOFF1 checks if data starts with the WOFF1 signature.
func isWOFF1(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	sig := binary.BigEndian.Uint32(data[:4])
	return sig == woffSignature
}

// woffHeader is the WOFF file header (44 bytes).
type woffHeader struct {
	Signature      uint32
	Flavor         uint32
	Length         uint32
	NumTables      uint16
	Reserved       uint16
	TotalSFNTSize  uint32
	MajorVersion   uint16
	MinorVersion   uint16
	MetaOffset     uint32
	MetaLength     uint32
	MetaOrigLength uint32
	PrivOffset     uint32
	PrivLength     uint32
}

// woffTableDir is a WOFF table directory entry (20 bytes).
type woffTableDir struct {
	Tag            uint32
	Offset         uint32
	CompLength     uint32
	OrigLength     uint32
	OrigChecksum   uint32
}

// decompressWOFF1 converts WOFF1 data to standard TrueType/OpenType data.
func decompressWOFF1(data []byte) ([]byte, error) {
	if len(data) < 44 {
		return nil, fmt.Errorf("WOFF data too short for header")
	}

	r := bytes.NewReader(data)
	var hdr woffHeader
	if err := binary.Read(r, binary.BigEndian, &hdr); err != nil {
		return nil, fmt.Errorf("reading WOFF header: %w", err)
	}
	if hdr.Signature != woffSignature {
		return nil, fmt.Errorf("invalid WOFF signature")
	}

	// Read table directory entries
	tables := make([]woffTableDir, hdr.NumTables)
	for i := 0; i < int(hdr.NumTables); i++ {
		if err := binary.Read(r, binary.BigEndian, &tables[i]); err != nil {
			return nil, fmt.Errorf("reading table dir %d: %w", i, err)
		}
	}

	// Build the output TrueType/OpenType file
	// OT header: 12 bytes + 16 bytes per table
	numTables := int(hdr.NumTables)
	headerSize := 12 + 16*numTables

	// Calculate search parameters for TrueType header
	searchRange := 1
	entrySelector := 0
	for searchRange*2 <= numTables {
		searchRange *= 2
		entrySelector++
	}
	searchRange *= 16
	rangeShift := numTables*16 - searchRange

	// Decompress all tables first
	tableData := make([][]byte, numTables)
	for i, t := range tables {
		if t.CompLength == t.OrigLength {
			// Not compressed — raw copy
			start := int(t.Offset)
			end := start + int(t.OrigLength)
			if end > len(data) {
				return nil, fmt.Errorf("table %d extends past WOFF data", i)
			}
			tableData[i] = data[start:end]
		} else {
			// zlib compressed
			start := int(t.Offset)
			end := start + int(t.CompLength)
			if end > len(data) {
				return nil, fmt.Errorf("table %d compressed data extends past WOFF data", i)
			}
			zr, err := zlib.NewReader(bytes.NewReader(data[start:end]))
			if err != nil {
				return nil, fmt.Errorf("zlib init for table %d: %w", i, err)
			}
			decompressed, err := io.ReadAll(zr)
			zr.Close()
			if err != nil {
				return nil, fmt.Errorf("zlib decompress for table %d: %w", i, err)
			}
			tableData[i] = decompressed
		}
	}

	// Calculate table offsets (4-byte aligned)
	offset := uint32(headerSize)
	tableOffsets := make([]uint32, numTables)
	for i, td := range tableData {
		tableOffsets[i] = offset
		offset += uint32(len(td))
		// Pad to 4-byte boundary
		if offset%4 != 0 {
			offset += 4 - (offset % 4)
		}
	}

	// Write output
	out := make([]byte, offset)
	// TrueType header
	binary.BigEndian.PutUint32(out[0:], hdr.Flavor)
	binary.BigEndian.PutUint16(out[4:], hdr.NumTables)
	binary.BigEndian.PutUint16(out[6:], uint16(searchRange))
	binary.BigEndian.PutUint16(out[8:], uint16(entrySelector))
	binary.BigEndian.PutUint16(out[10:], uint16(rangeShift))

	// Table directory entries
	for i, t := range tables {
		dirOffset := 12 + 16*i
		binary.BigEndian.PutUint32(out[dirOffset:], t.Tag)
		binary.BigEndian.PutUint32(out[dirOffset+4:], t.OrigChecksum)
		binary.BigEndian.PutUint32(out[dirOffset+8:], tableOffsets[i])
		binary.BigEndian.PutUint32(out[dirOffset+12:], t.OrigLength)
	}

	// Table data
	for i, td := range tableData {
		copy(out[tableOffsets[i]:], td)
	}

	return out, nil
}
