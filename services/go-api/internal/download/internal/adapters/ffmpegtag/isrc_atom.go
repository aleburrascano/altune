package ffmpegtag

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const (
	boxHeaderSize    = 8
	stcoEntryWidth   = 4
	co64EntryWidth   = 8
	chunkTableHeader = 16
	versionFlagsSize = 4
)

type box struct {
	start, end int
	kind       string
}

func childBoxes(mp4 []byte, from, to int) ([]box, error) {
	var boxes []box
	for pos := from; pos < to; {
		if to-pos < boxHeaderSize {
			return nil, errors.New("truncated box header")
		}
		size := int(binary.BigEndian.Uint32(mp4[pos:]))
		if size < 8 || pos+size > to {
			return nil, fmt.Errorf("unsupported box size %d at %d", size, pos)
		}
		boxes = append(boxes, box{start: pos, end: pos + size, kind: string(mp4[pos+4 : pos+8])})
		pos += size
	}
	return boxes, nil
}

func findBox(mp4 []byte, from, to int, kind string) (box, error) {
	boxes, err := childBoxes(mp4, from, to)
	if err != nil {
		return box{}, err
	}
	for _, b := range boxes {
		if b.kind == kind {
			return b, nil
		}
	}
	return box{}, fmt.Errorf("no %s box", kind)
}

func freeformAtom(name, value string) []byte {
	leaf := func(kind string, payload []byte) []byte {
		out := make([]byte, boxHeaderSize, boxHeaderSize+len(payload))
		binary.BigEndian.PutUint32(out, uint32(boxHeaderSize+len(payload)))
		copy(out[4:], kind)
		return append(out, payload...)
	}
	mean := leaf("mean", append(make([]byte, versionFlagsSize), "com.apple.iTunes"...))
	nameBox := leaf("name", append(make([]byte, versionFlagsSize), name...))
	payload := leaf("data", append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, value...))
	return leaf("----", append(append(mean, nameBox...), payload...))
}

func chunkTables(mp4 []byte, trak box) ([]box, error) {
	node := trak
	for _, kind := range []string{"mdia", "minf", "stbl"} {
		next, err := findBox(mp4, node.start+boxHeaderSize, node.end, kind)
		if err != nil {
			return nil, err
		}
		node = next
	}
	boxes, err := childBoxes(mp4, node.start+boxHeaderSize, node.end)
	if err != nil {
		return nil, err
	}
	var tables []box
	for _, b := range boxes {
		if b.kind == "stco" || b.kind == "co64" {
			tables = append(tables, b)
		}
	}
	return tables, nil
}

func shiftTable(mp4 []byte, table box, at int, delta uint32) error {
	width := stcoEntryWidth
	if table.kind == "co64" {
		width = co64EntryWidth
	}
	count := int(binary.BigEndian.Uint32(mp4[table.start+12:]))
	first := table.start + chunkTableHeader
	if first+count*width > table.end {
		return fmt.Errorf("%s table overruns its box", table.kind)
	}
	for i := range count {
		entry := mp4[first+i*width:]
		switch width {
		case stcoEntryWidth:
			if offset := binary.BigEndian.Uint32(entry); int(offset) >= at {
				binary.BigEndian.PutUint32(entry, offset+delta)
			}
		default:
			if offset := binary.BigEndian.Uint64(entry); int(offset) >= at {
				binary.BigEndian.PutUint64(entry, offset+uint64(delta))
			}
		}
	}
	return nil
}

func moovBox(mp4 []byte) (box, error) {
	return findBox(mp4, 0, len(mp4), "moov")
}

func ilstChain(mp4 []byte) ([]box, error) {
	moov, err := moovBox(mp4)
	if err != nil {
		return nil, err
	}
	chain := []box{moov}
	for _, kind := range []string{"udta", "meta", "ilst"} {
		parent := chain[len(chain)-1]
		from := parent.start + boxHeaderSize
		if parent.kind == "meta" {
			from += 4
		}
		next, err := findBox(mp4, from, parent.end, kind)
		if err != nil {
			return nil, err
		}
		chain = append(chain, next)
	}
	return chain, nil
}

func shiftAllChunkOffsets(mp4 []byte, moov box, at int, delta uint32) error {
	children, err := childBoxes(mp4, moov.start+boxHeaderSize, moov.end)
	if err != nil {
		return err
	}
	for _, trak := range children {
		if trak.kind != "trak" {
			continue
		}
		tables, err := chunkTables(mp4, trak)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if err := shiftTable(mp4, table, at, delta); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertAt(mp4 []byte, at int, insert []byte) []byte {
	out := make([]byte, 0, len(mp4)+len(insert))
	out = append(out, mp4[:at]...)
	out = append(out, insert...)
	return append(out, mp4[at:]...)
}

func embedISRC(path, isrc string) error {
	if isrc == "" {
		return nil
	}
	mp4, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the file to tag: %w", err)
	}
	chain, err := ilstChain(mp4)
	if err != nil {
		return err
	}
	ilst := chain[len(chain)-1]
	atom := freeformAtom("ISRC", isrc)
	delta := uint32(len(atom))
	if err := shiftAllChunkOffsets(mp4, chain[0], ilst.end, delta); err != nil {
		return err
	}
	for _, b := range chain {
		binary.BigEndian.PutUint32(mp4[b.start:], uint32(b.end-b.start)+delta)
	}
	if err := os.WriteFile(path, insertAt(mp4, ilst.end, atom), 0o600); err != nil {
		return fmt.Errorf("write the tagged file: %w", err)
	}
	return nil
}
