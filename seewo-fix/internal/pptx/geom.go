package pptx

import "strconv"

// Geometry constants.
const (
	EMUPerPt   = 12700
	EMUPerTwip = 635 // 914400 / 1440
)

// Mode says how a reader is assumed to handle a group's child coordinate space
// (a:chOff / a:chExt).
type Mode int

const (
	// Spec is correct OOXML behaviour: children are remapped by ext/chExt.
	Spec Mode = iota
	// IgnoreChSpace is a reader that takes a child's off/ext as absolute EMU —
	// effectively what a converter written against PowerPoint output does,
	// because PowerPoint always writes the identity transform.
	IgnoreChSpace
	// NoScale is a reader that applies the group's translation but not the
	// scale: abs = off + (child - chOff).
	NoScale
)

// Vals are the numbers of one a:xfrm. Missing members are zero and reported
// by the ok flags.
type Vals struct {
	OffX, OffY, ExtX, ExtY             int64
	ChOffX, ChOffY, ChExtX, ChExtY     int64
	HasOff, HasExt, HasChOff, HasChExt bool
}

// Read parses the numbers of an xfrm out of the current bytes.
func (x *Xfrm) Read(data []byte) Vals {
	var v Vals
	get := func(key string) (int64, bool) {
		a, ok := x.Attrs[key]
		if !ok {
			return 0, false
		}
		n, err := strconv.ParseInt(string(data[a.ValStart:a.ValEnd]), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
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

// Scale is ext/chExt of a group.
func (v Vals) Scale() (sx, sy float64, ok bool) {
	if v.ChExtX <= 0 || v.ChExtY <= 0 {
		return 1, 1, false
	}
	return float64(v.ExtX) / float64(v.ChExtX), float64(v.ExtY) / float64(v.ChExtY), true
}

// IsIdentity reports a group transform that needs no work: chOff == off and
// chExt == ext, the shape PowerPoint itself writes.
func (v Vals) IsIdentity() bool {
	return v.HasChOff && v.HasChExt &&
		v.ChOffX == v.OffX && v.ChOffY == v.OffY &&
		v.ChExtX == v.ExtX && v.ChExtY == v.ExtY
}

// Geom is one shape's absolute rectangle.
type Geom struct {
	Kind string
	ID   string
	Rect Rect
}

// Geoms computes the absolute rectangle of every shape in a part under one Mode.
func Geoms(d *Doc, mode Mode) []Geom {
	var out []Geom
	type frame struct{ sx, tx, sy, ty float64 }

	var walk func(s *Shape, f frame)
	walk = func(s *Shape, f frame) {
		if s.Xfrm == nil {
			for _, ch := range s.Children {
				walk(ch, f)
			}
			return
		}
		v := s.Xfrm.Read(d.Data)
		if !v.HasOff || !v.HasExt {
			for _, ch := range s.Children {
				walk(ch, f)
			}
			return
		}
		sx, tx, sy, ty := f.sx, f.tx, f.sy, f.ty
		rect := Rect{
			X:  int64(float64(v.OffX)*sx + tx),
			Y:  int64(float64(v.OffY)*sy + ty),
			CX: int64(float64(v.ExtX) * sx),
			CY: int64(float64(v.ExtY) * sy),
		}
		out = append(out, Geom{Kind: s.Kind, ID: s.ID, Rect: rect})

		if s.Kind != "p:grpSp" || !v.HasChExt || v.ChExtX <= 0 || v.ChExtY <= 0 {
			for _, ch := range s.Children {
				walk(ch, f)
			}
			return
		}
		switch mode {
		case Spec:
			nsx := float64(rect.CX) / float64(v.ChExtX)
			nsy := float64(rect.CY) / float64(v.ChExtY)
			nf := frame{nsx, float64(rect.X) - float64(v.ChOffX)*nsx,
				nsy, float64(rect.Y) - float64(v.ChOffY)*nsy}
			for _, ch := range s.Children {
				walk(ch, nf)
			}
		case IgnoreChSpace:
			for _, ch := range s.Children {
				walk(ch, frame{1, 0, 1, 0})
			}
		case NoScale:
			for _, ch := range s.Children {
				walk(ch, frame{1, float64(rect.X) - float64(v.ChOffX),
					1, float64(rect.Y) - float64(v.ChOffY)})
			}
		}
	}
	for _, s := range d.Roots {
		walk(s, frame{1, 0, 1, 0})
	}
	return out
}

// Groups lists every group of a part with its transform.
type Group struct {
	ID  string
	SX  float64
	SY  float64
	OK  bool
	Idt bool
}

// Groups reports the transform of every group in a part.
func Groups(d *Doc) []Group {
	var out []Group
	var walk func(s *Shape)
	walk = func(s *Shape) {
		if s.Kind == "p:grpSp" && s.Xfrm != nil {
			v := s.Xfrm.Read(d.Data)
			sx, sy, ok := v.Scale()
			out = append(out, Group{ID: s.ID, SX: sx, SY: sy, OK: ok, Idt: v.IsIdentity()})
		}
		for _, ch := range s.Children {
			walk(ch)
		}
	}
	for _, s := range d.Roots {
		walk(s)
	}
	return out
}
