package pptx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// PowerPoint's text-box insets, written explicitly so a reader cannot invent
// its own.
const (
	defaultLIns = 91440
	defaultTIns = 45720
	defaultRIns = 91440
	defaultBIns = 45720
)

// Options selects what the fix does. The zero value is "nothing".
type Options struct {
	// Groups bakes every group transform into its children and re-bases the
	// group onto itself, so every group transform becomes the identity — the
	// only form PowerPoint ever writes, and the only one a PowerPoint-only
	// converter is known to handle.
	Groups bool
	// Ungroup removes the group wrappers after baking, leaving a flat shape
	// tree. Strongest option for a converter that mishandles groups at all.
	Ungroup bool
	// NoAutofit replaces <a:spAutoFit/> with <a:noAutofit/> so a converter does
	// not re-measure text and resize the box.
	NoAutofit bool
	// Insets writes lIns/tIns/rIns/bIns where a body leaves them implicit.
	Insets bool
	// DX, DY shift every shape, in points. For compensating a constant offset
	// that survives the other fixes.
	DX, DY float64
	// TextDX, TextDY, TextDW, TextDH move and resize text boxes only, in
	// points, to undo what a converter does to them. Text boxes inside a group
	// are only handled once the group transforms are canonical (Groups).
	TextDX, TextDY, TextDW, TextDH float64
	// Scale multiplies every shape's rectangle about the page origin, 0 or 1
	// meaning no change. It compensates a reader that maps the page onto a
	// canvas of another size. Text keeps its size, so a compensated deck is a
	// diagnostic: boxes right, type the size it was written at.
	Scale float64
	// Overlays rebuilds text boxes laid over part of another text box's line
	// (a coloured copy of some words, revealed by animation) so both lay out
	// the same string and stay aligned under any renderer. See overlay.go.
	Overlays bool
	// Lines makes nearly level (or vertical) lines exactly so. See lines.go.
	Lines bool
}

// Any reports whether any change is requested.
func (o Options) Any() bool {
	return o.Overlays || o.Lines || o.Groups || o.Ungroup || o.NoAutofit || o.Insets || o.DX != 0 || o.DY != 0 || (o.Scale != 0 && o.Scale != 1) ||
		o.TextDX != 0 || o.TextDY != 0 || o.TextDW != 0 || o.TextDH != 0
}

// PartStats describes what happened to one part.
type PartStats struct {
	Parts             int
	Groups            int  // groups seen
	NonIdent          int  // groups whose transform was not the identity
	Baked             int  // groups normalised
	Ungrouped         int  // groups flattened away
	Autofits          int  // spAutoFit elements frozen
	InsetBodies       int  // bodyPr elements given explicit insets
	ShapesMoved       int  // shapes shifted
	TextBoxesAdjusted int  // text boxes moved or resized
	Scaled            bool // geometry scaled
	Overlays          int  // overlay text boxes rebuilt
	Lines             int  // lines straightened
	OverlayNotes      []string
	Warnings          []string
}

func (s *PartStats) merge(o PartStats) {
	s.Parts += o.Parts
	s.Groups += o.Groups
	s.NonIdent += o.NonIdent
	s.Baked += o.Baked
	s.Ungrouped += o.Ungrouped
	s.Autofits += o.Autofits
	s.InsetBodies += o.InsetBodies
	s.ShapesMoved += o.ShapesMoved
	s.TextBoxesAdjusted += o.TextBoxesAdjusted
	s.Scaled = s.Scaled || o.Scaled
	s.Overlays += o.Overlays
	s.Lines += o.Lines
	s.OverlayNotes = append(s.OverlayNotes, o.OverlayNotes...)
	s.Warnings = append(s.Warnings, o.Warnings...)
}

// editor applies value overrides on top of the original bytes. Value edits are
// keyed by span so an attribute written twice keeps one edit (the last value);
// structural edits (insertions, deletions) are kept apart because they may
// contain value edits of their own.
type editor struct {
	data  []byte
	cur   map[int]int64 // ValStart -> value after the edits made so far
	val   map[int]Edit  // ValStart -> the edit for that attribute
	other []Edit
	warn  []string
}

func newEditor(data []byte) *editor {
	return &editor{data: data, cur: map[int]int64{}, val: map[int]Edit{}}
}

func (e *editor) edits() []Edit {
	out := make([]Edit, 0, len(e.val)+len(e.other))
	for _, ed := range e.val {
		out = append(out, ed)
	}
	out = append(out, e.other...)
	SortEdits(out)
	return out
}

func (e *editor) get(a Attr) (int64, bool) {
	if v, ok := e.cur[a.ValStart]; ok {
		return v, true
	}
	n, err := strconv.ParseInt(string(e.data[a.ValStart:a.ValEnd]), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (e *editor) set(a Attr, v int64) bool {
	if old, ok := e.get(a); ok && old == v {
		return false
	}
	e.cur[a.ValStart] = v
	e.val[a.ValStart] = Edit{a.ValStart, a.ValEnd, strconv.FormatInt(v, 10)}
	return true
}

// FixPart applies the options to one XML part.
func FixPart(data []byte, opt Options) ([]byte, PartStats, error) {
	var st PartStats
	doc, err := Scan(data)
	if err != nil {
		return nil, st, err
	}
	st.Parts = 1
	for _, g := range Groups(doc) {
		st.Groups++
		if !g.Idt {
			st.NonIdent++
		}
	}

	if !opt.Any() {
		return data, st, nil
	}

	e := newEditor(data)

	if opt.Groups || opt.Ungroup {
		var walk func(s *Shape)
		walk = func(s *Shape) {
			if s.Kind == "p:grpSp" && s.Xfrm != nil {
				e.bakeGroup(s, &st, opt.Ungroup)
			}
			for _, ch := range s.Children {
				walk(ch)
			}
		}
		for _, s := range doc.Roots {
			walk(s)
		}
	}

	if opt.Ungroup {
		var flatten func(s *Shape)
		flatten = func(s *Shape) {
			if s.Kind == "p:grpSp" && s.PrefixEnd > 0 {
				e.other = append(e.other,
					Edit{s.TagStart, s.PrefixEnd, ""},
					Edit{s.EndStart, s.EndEnd, ""})
				st.Ungrouped++
			}
			for _, ch := range s.Children {
				flatten(ch)
			}
		}
		for _, s := range doc.Roots {
			flatten(s)
		}
	}

	if opt.NoAutofit {
		for _, span := range doc.AutoFits {
			tag := string(data[span[0]:span[1]])
			if !strings.Contains(tag, "spAutoFit") {
				continue
			}
			e.other = append(e.other, Edit{span[0], span[1],
				strings.Replace(tag, "spAutoFit", "noAutofit", 1)})
			st.Autofits++
		}
	}

	if opt.Insets {
		for _, span := range doc.BodyPrs {
			tag := data[span[0]:span[1]]
			if bytes.Contains(tag, []byte("lIns=")) {
				continue
			}
			insertAt := span[0] + len("<a:bodyPr")
			ins := fmt.Sprintf(` lIns="%d" tIns="%d" rIns="%d" bIns="%d"`,
				defaultLIns, defaultTIns, defaultRIns, defaultBIns)
			e.other = append(e.other, Edit{insertAt, insertAt, ins})
			st.InsetBodies++
		}
	}

	if opt.TextDX != 0 || opt.TextDY != 0 || opt.TextDW != 0 || opt.TextDH != 0 {
		dx := int64(math.Round(opt.TextDX * EMUPerPt))
		dy := int64(math.Round(opt.TextDY * EMUPerPt))
		dw := int64(math.Round(opt.TextDW * EMUPerPt))
		dh := int64(math.Round(opt.TextDH * EMUPerPt))
		var walk func(s *Shape)
		walk = func(s *Shape) {
			if s.IsTextBox && s.HasText && s.Xfrm != nil {
				shift(s.Xfrm, e, dx, dy, dw, dh)
				st.TextBoxesAdjusted++
			}
			for _, ch := range s.Children {
				walk(ch)
			}
		}
		for _, s := range doc.Roots {
			walk(s)
		}
	}

	if opt.Scale != 0 && opt.Scale != 1 {
		var walk func(s *Shape)
		walk = func(s *Shape) {
			if s.Xfrm != nil && !(opt.Ungroup && s.Kind == "p:grpSp") {
				for _, key := range []string{"off.x", "off.y", "ext.cx", "ext.cy", "chOff.x", "chOff.y", "chExt.cx", "chExt.cy"} {
					if a, ok := s.Xfrm.Attrs[key]; ok {
						if v, ok := e.get(a); ok {
							e.set(a, int64(math.Round(float64(v)*opt.Scale)))
						}
					}
				}
			}
			for _, ch := range s.Children {
				walk(ch)
			}
		}
		for _, s := range doc.Roots {
			walk(s)
		}
		st.Scaled = true
	}

	if opt.DX != 0 || opt.DY != 0 {
		dx := int64(math.Round(opt.DX * EMUPerPt))
		dy := int64(math.Round(opt.DY * EMUPerPt))
		var shiftRoots []*Shape
		if opt.Ungroup {
			var collect func(s *Shape)
			collect = func(s *Shape) {
				if s.Kind != "p:grpSp" {
					shiftRoots = append(shiftRoots, s)
					return
				}
				for _, ch := range s.Children {
					collect(ch)
				}
			}
			for _, s := range doc.Roots {
				collect(s)
			}
		} else {
			shiftRoots = doc.Roots
		}
		for _, s := range shiftRoots {
			if s.Xfrm == nil {
				continue
			}
			moved := false
			if a, ok := s.Xfrm.Attrs["off.x"]; ok {
				if v, ok := e.get(a); ok {
					moved = e.set(a, v+dx) || moved
				}
			}
			if a, ok := s.Xfrm.Attrs["off.y"]; ok {
				if v, ok := e.get(a); ok {
					moved = e.set(a, v+dy) || moved
				}
			}
			// After baking, a group's chOff must follow its off.
			if s.Kind == "p:grpSp" && !opt.Ungroup {
				if a, ok := s.Xfrm.Attrs["chOff.x"]; ok {
					if v, ok := e.get(a); ok {
						e.set(a, v+dx)
					}
				}
				if a, ok := s.Xfrm.Attrs["chOff.y"]; ok {
					if v, ok := e.get(a); ok {
						e.set(a, v+dy)
					}
				}
			}
			if moved {
				st.ShapesMoved++
			}
		}
	}

	st.Warnings = append(st.Warnings, e.warn...)
	return Apply(data, e.edits()), st, nil
}

// bakeGroup turns one group's transform into the identity by rewriting its
// children's geometry into absolute EMU and re-basing chOff/chExt onto off/ext.
func (e *editor) bakeGroup(s *Shape, st *PartStats, willBeDeleted bool) {
	x := s.Xfrm
	if x.rot {
		st.Warnings = append(st.Warnings, fmt.Sprintf("group %s: rotated/flipped group left as is", s.ID))
		return
	}
	v := e.readXfrm(x)
	sx, sy, ok := v.Scale()
	if !ok {
		st.Warnings = append(st.Warnings, fmt.Sprintf("group %s: degenerate chExt", s.ID))
		return
	}
	if !v.HasOff || !v.HasExt || !v.HasChOff {
		st.Warnings = append(st.Warnings, fmt.Sprintf("group %s: incomplete transform", s.ID))
		return
	}
	for _, ch := range s.Children {
		if ch.Xfrm == nil {
			st.Warnings = append(st.Warnings, fmt.Sprintf("group %s: child %s has no transform", s.ID, ch.ID))
			continue
		}
		cv := e.readXfrm(ch.Xfrm)
		if !cv.HasOff || !cv.HasExt {
			continue
		}
		e.set(ch.Xfrm.Attrs["off.x"], int64(math.Round(float64(v.OffX)+(float64(cv.OffX-v.ChOffX))*sx)))
		e.set(ch.Xfrm.Attrs["off.y"], int64(math.Round(float64(v.OffY)+(float64(cv.OffY-v.ChOffY))*sy)))
		e.set(ch.Xfrm.Attrs["ext.cx"], int64(math.Round(float64(cv.ExtX)*sx)))
		e.set(ch.Xfrm.Attrs["ext.cy"], int64(math.Round(float64(cv.ExtY)*sy)))
	}
	if !willBeDeleted {
		// Re-base the group onto itself: chOff := off, chExt := ext.
		e.set(x.Attrs["chOff.x"], v.OffX)
		e.set(x.Attrs["chOff.y"], v.OffY)
		e.set(x.Attrs["chExt.cx"], v.ExtX)
		e.set(x.Attrs["chExt.cy"], v.ExtY)
	}
	st.Baked++
}

// shift moves and resizes one transform, keeping a group's child space with it.
func shift(x *Xfrm, e *editor, dx, dy, dw, dh int64) {
	for _, k := range []struct {
		key string
		d   int64
	}{{"off.x", dx}, {"off.y", dy}, {"ext.cx", dw}, {"ext.cy", dh},
		{"chOff.x", dx}, {"chOff.y", dy}, {"chExt.cx", dw}, {"chExt.cy", dh}} {
		if a, ok := x.Attrs[k.key]; ok {
			if v, ok := e.get(a); ok {
				e.set(a, v+k.d)
			}
		}
	}
}

// readXfrm reads an xfrm with the overrides made so far applied.
func (e *editor) readXfrm(x *Xfrm) Vals {
	var v Vals
	get := func(key string) (int64, bool) {
		a, ok := x.Attrs[key]
		if !ok {
			return 0, false
		}
		return e.get(a)
	}
	v.OffX, v.HasOff = get("off.x")
	v.OffY, _ = get("off.y")
	v.ExtX, v.HasExt = get("ext.cx")
	v.ExtY, _ = get("ext.cy")
	v.ChOffX, v.HasChOff = get("chOff.x")
	v.ChOffY, _ = get("chOff.y")
	v.ChExtX, v.HasChExt = get("chExt.cx")
	v.ChExtY, _ = get("chExt.cy")
	return v
}

// Texts returns every text run of a part, in document order.
func Texts(data []byte) ([]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var out []string
	depth := 0
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if fullName(t.Name) == "a:t" {
				depth++
			}
		case xml.EndElement:
			if fullName(t.Name) == "a:t" {
				depth--
			}
		case xml.CharData:
			if depth > 0 {
				out = append(out, string(t))
			}
		}
	}
}

// The text-box correction 希沃 needs: none.
//
// A deck imported by 希沃 and exported again as .enbx differs for plain text
// boxes by exactly (lIns-10, tIns-10, +20, +20) pixels at 96 dpi. That is not an
// error: 希沃's text element draws its content with a fixed 10 px padding
// (`updateTextProperty(id, {…, padding: 10})` in the client), so the converter
// grows the box by that padding and the glyphs land on off+lIns, exactly where
// PowerPoint draws them. Shifting text boxes in the deck therefore moves the
// text away from the shapes drawn around it. The constants stay at 0 so that
// callers keep compiling; see calibrate.go for the residual check.
const (
	DefaultTextDX = 0.0
	DefaultTextDY = 0.0
)
