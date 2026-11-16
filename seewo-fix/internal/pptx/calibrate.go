package pptx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"

	"seewo-fix/internal/enbx"
)

// EMUPerPx is one pixel at 96 dpi, the unit 希沃's page uses.
const EMUPerPx = 9525

// DeckShape is one drawable of a deck with its absolute rectangle.
type DeckShape struct {
	Slide     int    // 0-based page order
	ID        string // cNvPr id, what the converter records as OriginPptxId
	Kind      string // p:sp, p:pic, p:grpSp, ...
	Name      string
	InGroup   bool
	IsTextBox bool
	HasText   bool
	Insets    [4]int64
	Rect      Rect
}

// Shapes lists every drawable of a deck in page order.
func Shapes(data []byte) ([]DeckShape, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	var names []string
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		if IsShapePart(f.Name) && bytes.HasPrefix([]byte(f.Name), []byte("ppt/slides/")) {
			names = append(names, f.Name)
			files[f.Name] = f
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })

	var out []DeckShape
	for slide, name := range names {
		rc, err := files[name].Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		doc, err := Scan(body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		var walk func(s *Shape, f transform, inGroup bool)
		walk = func(s *Shape, f transform, inGroup bool) {
			if s.Xfrm != nil {
				v := s.Xfrm.Read(doc.Data)
				if v.HasOff && v.HasExt {
					out = append(out, DeckShape{
						Slide: slide, ID: s.ID, Kind: s.Kind,
						InGroup: inGroup, IsTextBox: s.IsTextBox, HasText: s.HasText,
						Insets: s.Insets,
						Rect: Rect{
							X:  int64(float64(v.OffX)*f.sx + f.tx),
							Y:  int64(float64(v.OffY)*f.sy + f.ty),
							CX: int64(float64(v.ExtX) * f.sx),
							CY: int64(float64(v.ExtY) * f.sy),
						},
					})
				}
				if s.Kind == "p:grpSp" && v.HasChExt && v.ChExtX > 0 && v.ChExtY > 0 {
					ax := float64(v.OffX)*f.sx + f.tx
					ay := float64(v.OffY)*f.sy + f.ty
					acx := float64(v.ExtX) * f.sx
					acy := float64(v.ExtY) * f.sy
					nsx, nsy := acx/float64(v.ChExtX), acy/float64(v.ChExtY)
					for _, ch := range s.Children {
						walk(ch, transform{nsx, ax - float64(v.ChOffX)*nsx, nsy, ay - float64(v.ChOffY)*nsy}, true)
					}
					return
				}
			}
			for _, ch := range s.Children {
				walk(ch, f, inGroup)
			}
		}
		for _, s := range doc.Roots {
			walk(s, transform{1, 0, 1, 0}, false)
		}
	}
	return out, nil
}

type transform struct{ sx, tx, sy, ty float64 }

// ClassDelta is the measured difference between 希沃's own geometry and the
// deck's, for one class of element, in pixels at 96 dpi.
type ClassDelta struct {
	Class          string // "Text", "Shape", …
	N              int
	DX, DY, DW, DH float64 // median, px at 96 dpi
	MinDX, MaxDX   float64
	// LIns, TIns are the median text insets of the matched shapes, px.
	LIns, TIns float64
	// Residual is what is left after taking the converter's expected rewrite
	// into account, px: X, Y. For plain text that rewrite is (lIns-10, tIns-10)
	// because 希沃 pads its text element by 10 px; for everything else it is 0.
	// A real conversion leaves ~0 here.
	Residual [2]float64
	// Suggest is kept for compatibility and is always zero: the conversion is
	// already exact, so there is nothing to pre-compensate.
	Suggest [4]float64
}

// SeewoTextPadding is the fixed padding of 希沃's text element, px at 96 dpi.
const SeewoTextPadding = 10.0

// MismatchPx is how far a residual may stray before the enbx is taken to come
// from a different version of the deck.
const MismatchPx = 1.5

// Calibration is the measurement of one conversion.
type Calibration struct {
	Slides  int
	Classes []ClassDelta
	// Text is the delta measured on text boxes alone, nil when the deck has
	// none in the enbx.
	Text *ClassDelta
	// Missing counts deck shapes the enbx has no element for.
	Missing int
	// Mismatch is set when elements sit consistently away from where the
	// converter puts them: the enbx was exported from another version of the
	// deck (for instance an earlier .seewo.pptx), so it says nothing about this
	// file.
	Mismatch bool
}

// Calibrate compares a deck with the .enbx 希沃 produced from it.
//
// It matches elements by the PowerPoint shape id the converter recorded, so it
// measures the conversion itself, not a guess about it.
func Calibrate(deck, enbxData []byte) (*Calibration, error) {
	shapes, err := Shapes(deck)
	if err != nil {
		return nil, err
	}
	edoc, err := enbx.Read(enbxData)
	if err != nil {
		return nil, err
	}
	// index deck shapes by slide and id
	idx := map[[2]interface{}]DeckShape{}
	bySlide := map[int]map[string]DeckShape{}
	for _, s := range shapes {
		if bySlide[s.Slide] == nil {
			bySlide[s.Slide] = map[string]DeckShape{}
		}
		bySlide[s.Slide][s.ID] = s
	}
	_ = idx

	c := &Calibration{Slides: len(edoc.Slides)}
	samples := map[string][]ClassDelta{}

	record := func(pa enbx.Element, ds DeckShape) {
		_ = pa
		if pa.X == 0 && pa.Y == 0 && pa.W == 0 && pa.H == 0 && pa.Origin == "" {
			return
		}
		dx := pa.X - float64(ds.Rect.X)/EMUPerPx
		dy := pa.Y - float64(ds.Rect.Y)/EMUPerPx
		dw := pa.W - float64(ds.Rect.CX)/EMUPerPx
		dh := pa.H - float64(ds.Rect.CY)/EMUPerPx
		class := pa.Kind
		ins := ClassDelta{Class: class, N: 1, DX: dx, DY: dy, DW: dw, DH: dh,
			LIns: float64(ds.Insets[0]) / EMUPerPx, TIns: float64(ds.Insets[1]) / EMUPerPx}
		samples[class] = append(samples[class], ins)
	}

	for _, sl := range edoc.Slides {
		deck := bySlide[sl.Index]
		if deck == nil {
			continue
		}
		for _, el := range sl.Elements {
			// A Group around a text box repeats the box's own origin id; the
			// children carry none, so compare both levels against the group.
			if ds, ok := deck[el.Origin]; ok && el.Origin != "" {
				if el.Kind == "Group" && ds.Kind != "p:grpSp" {
					continue // handled through its children below
				}
				record(el, ds)
			}
			for _, ch := range sl.Children[el.Origin] {
				if ch.Origin == "" {
					continue
				}
				ds, ok := deck[ch.Origin]
				if !ok {
					continue
				}
				// children are relative to the group, so compare relative
				rel := DeckShape{Rect: Rect{
					X:  ds.Rect.X - deck[el.Origin].Rect.X,
					Y:  ds.Rect.Y - deck[el.Origin].Rect.Y,
					CX: ds.Rect.CX, CY: ds.Rect.CY,
				}, ID: ds.ID, Kind: ds.Kind, Insets: ds.Insets}
				record(ch, rel)
			}
		}
	}

	for class, list := range samples {
		cd := medianOf(class, list)
		c.Classes = append(c.Classes, cd)
		if class == "Text" {
			c.Text = &cd
		}
		if math.Abs(cd.Residual[0]) > MismatchPx || math.Abs(cd.Residual[1]) > MismatchPx {
			c.Mismatch = true
		}
	}
	sort.Slice(c.Classes, func(i, j int) bool { return c.Classes[i].N > c.Classes[j].N })
	return c, nil
}

func medianOf(class string, list []ClassDelta) ClassDelta {
	get := func(f func(ClassDelta) float64) float64 {
		vals := make([]float64, len(list))
		for i, s := range list {
			vals[i] = f(s)
		}
		sort.Float64s(vals)
		n := len(vals)
		if n%2 == 1 {
			return vals[n/2]
		}
		return (vals[n/2-1] + vals[n/2]) / 2
	}
	cd := ClassDelta{
		Class: class, N: len(list),
		DX: get(func(s ClassDelta) float64 { return s.DX }),
		DY: get(func(s ClassDelta) float64 { return s.DY }),
		DW: get(func(s ClassDelta) float64 { return s.DW }),
		DH: get(func(s ClassDelta) float64 { return s.DH }),
	}
	cd.LIns = get(func(s ClassDelta) float64 { return s.LIns })
	cd.TIns = get(func(s ClassDelta) float64 { return s.TIns })
	// What a correct conversion produces: plain text is regrown around 希沃's
	// fixed padding, everything else is copied as is.
	ex, ey := 0.0, 0.0
	if class == "Text" {
		ex, ey = cd.LIns-SeewoTextPadding, cd.TIns-SeewoTextPadding
	}
	cd.Residual = [2]float64{cd.DX - ex, cd.DY - ey}
	cd.MinDX = math.Inf(1)
	for _, s := range list {
		if s.DX < cd.MinDX {
			cd.MinDX = s.DX
		}
		if s.DX > cd.MaxDX {
			cd.MaxDX = s.DX
		}
	}
	return cd
}
