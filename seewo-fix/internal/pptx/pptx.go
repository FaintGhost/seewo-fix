package pptx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
)

// partPattern selects the XML parts that can hold shapes.
var partPattern = regexp.MustCompile(`^ppt/(slides|slideLayouts|slideMasters|notesSlides)/[^/]+\.xml$`)

// slidePattern selects the slides themselves, where overlays are rebuilt.
var slidePattern = regexp.MustCompile(`^ppt/slides/slide\d+\.xml$`)

// IsShapePart reports whether a zip entry can hold a shape tree.
func IsShapePart(name string) bool { return partPattern.MatchString(name) }

func readParts(data []byte, fn func(name string, body []byte) ([]byte, error)) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		if IsShapePart(f.Name) {
			body, err = fn(f.Name, body)
			if err != nil {
				return nil, err
			}
		}
		hdr := &zip.FileHeader{
			Name:           f.Name,
			Method:         f.Method,
			Modified:       f.Modified,
			NonUTF8:        f.NonUTF8,
			ExternalAttrs:  f.ExternalAttrs,
			Extra:          f.Extra,
			Comment:        f.Comment,
			CreatorVersion: f.CreatorVersion,
			ReaderVersion:  f.ReaderVersion,
			Flags:          f.Flags &^ 0x8, // we always write sizes
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		if _, err := w.Write(body); err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Fix applies options to every shape part of a deck.
func Fix(data []byte, opt Options) ([]byte, PartStats, error) {
	var total PartStats
	out, err := readParts(data, func(name string, body []byte) ([]byte, error) {
		fixed, st, err := FixPart(body, opt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if opt.Lines && slidePattern.MatchString(name) {
			var ids []string
			fixed, ids, err = FixLines(fixed)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			st.Lines += len(ids)
		}
		if opt.Overlays && slidePattern.MatchString(name) {
			var ids, notes []string
			fixed, ids, notes, err = FixOverlays(fixed, SystemFonts)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			st.Overlays += len(ids)
			for _, n := range notes {
				st.OverlayNotes = append(st.OverlayNotes, fmt.Sprintf("第 %d 张 %s", partNumber(name), n))
			}
		}
		total.merge(st)
		return fixed, nil
	})
	return out, total, err
}

// PartAnalysis is the diagnosis of one part.
type PartAnalysis struct {
	Name      string
	Shapes    int
	Groups    []Group
	WorstSpec float64 // no meaning; kept for symmetry
	WorstIgn  float64 // pt, IgnoreChSpace vs Spec
	WorstNoSc float64 // pt, NoScale vs Spec
	WorstName string
}

// Analysis is the diagnosis of a whole deck.
type Analysis struct {
	Parts     []PartAnalysis
	Groups    int
	NonIdent  int
	WorstIgn  float64
	WorstNoSc float64
	Slides    int
}

// Analyze reports group transforms and the offset a broken reader would add.
func Analyze(data []byte) (*Analysis, error) {
	a := &Analysis{}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	names := make([]string, 0, len(zr.File))
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		if !IsShapePart(f.Name) {
			continue
		}
		names = append(names, f.Name)
		byName[f.Name] = f
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })

	for _, name := range names {
		f := byName[name]
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, "ppt/slides/") {
			a.Slides++
		}
		doc, err := Scan(body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		pa := PartAnalysis{Name: name, Groups: Groups(doc)}
		a.Groups += len(pa.Groups)
		for _, g := range pa.Groups {
			if !g.Idt {
				a.NonIdent++
			}
		}
		spec := Geoms(doc, Spec)
		ign := Geoms(doc, IgnoreChSpace)
		nos := Geoms(doc, NoScale)
		pa.Shapes = len(spec)
		if len(spec) == len(ign) && len(spec) == len(nos) {
			for i := range spec {
				dxI := math.Abs(float64(ign[i].Rect.X-spec[i].Rect.X) / EMUPerPt)
				dyI := math.Abs(float64(ign[i].Rect.Y-spec[i].Rect.Y) / EMUPerPt)
				if d := math.Max(dxI, dyI); d > pa.WorstIgn {
					pa.WorstIgn = d
					pa.WorstName = spec[i].ID
				}
				dxN := math.Abs(float64(nos[i].Rect.X-spec[i].Rect.X) / EMUPerPt)
				dyN := math.Abs(float64(nos[i].Rect.Y-spec[i].Rect.Y) / EMUPerPt)
				pa.WorstNoSc = math.Max(pa.WorstNoSc, math.Max(dxN, dyN))
			}
		}
		a.WorstIgn = math.Max(a.WorstIgn, pa.WorstIgn)
		a.WorstNoSc = math.Max(a.WorstNoSc, pa.WorstNoSc)
		a.Parts = append(a.Parts, pa)
	}
	return a, nil
}

// VerifyResult is the proof that a patched deck renders the same.
type VerifyResult struct {
	Parts        int
	GeomDriftEMU int64    // worst |Δ| of any rect member, Spec before vs Spec after
	TextDiffs    []string // parts whose text changed
	GroupsLeft   int      // groups still non-identity after the patch
	ResidualIgn  float64  // pt, IgnoreChSpace(after) vs Spec(before)
	ResidualNoSc float64  // pt, NoScale(after) vs Spec(before)
	EntryDiffs   []string // entries added, removed or reordered
	OtherDiffs   []string // non-shape entries whose content changed
}

// Verify compares a deck with its patched copy, expecting the geometry to be
// preserved within 2 EMU.
func Verify(before, after []byte) (*VerifyResult, error) {
	return VerifyAllowing(before, after, 2)
}

// VerifyAllowing is Verify with a tolerance, for a patch that deliberately
// moves shapes, as a text compensation measured against 希沃 does.
func VerifyAllowing(before, after []byte, allowedEMU int64) (*VerifyResult, error) {
	res := &VerifyResult{}
	zb, err := zip.NewReader(bytes.NewReader(before), int64(len(before)))
	if err != nil {
		return nil, err
	}
	za, err := zip.NewReader(bytes.NewReader(after), int64(len(after)))
	if err != nil {
		return nil, err
	}
	bodyOf := func(z *zip.Reader) (map[string][]byte, []string, error) {
		m := map[string][]byte{}
		var order []string
		for _, f := range z.File {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, nil, err
			}
			m[f.Name] = b
			order = append(order, f.Name)
		}
		return m, order, nil
	}
	mb, ob, err := bodyOf(zb)
	if err != nil {
		return nil, err
	}
	ma, oa, err := bodyOf(za)
	if err != nil {
		return nil, err
	}
	if len(ob) != len(oa) {
		res.EntryDiffs = append(res.EntryDiffs, fmt.Sprintf("entry count %d -> %d", len(ob), len(oa)))
	}
	for i := range ob {
		if i < len(oa) && ob[i] != oa[i] {
			res.EntryDiffs = append(res.EntryDiffs, fmt.Sprintf("order %d: %s -> %s", i, ob[i], oa[i]))
		}
	}
	for name, b := range mb {
		a, ok := ma[name]
		if !ok {
			res.EntryDiffs = append(res.EntryDiffs, "missing after: "+name)
			continue
		}
		if !IsShapePart(name) {
			if !bytes.Equal(b, a) {
				res.OtherDiffs = append(res.OtherDiffs, name)
			}
			continue
		}
		res.Parts++
		docB, err := Scan(b)
		if err != nil {
			return nil, fmt.Errorf("%s (before): %w", name, err)
		}
		docA, err := Scan(a)
		if err != nil {
			return nil, fmt.Errorf("%s (after): %w", name, err)
		}
		specB := drawable(Geoms(docB, Spec))
		specA := drawable(Geoms(docA, Spec))
		// Rebuilt overlays take their base's frame on purpose.
		rebuilt := map[string]bool{}
		for _, s := range docA.Roots {
			if s.Kind == "p:sp" && bytes.Contains(a[s.TagStart:s.EndEnd], []byte(TransparentFill)) &&
				!bytes.Contains(b, a[s.TagStart:s.EndEnd]) {
				rebuilt[s.ID] = true
			}
		}
		if len(specB) != len(specA) {
			res.EntryDiffs = append(res.EntryDiffs, fmt.Sprintf("%s: shape count %d -> %d", name, len(specB), len(specA)))
			continue
		}
		// Straightened lines move by at most half their drift.
		straight := map[string]bool{}
		for _, g := range specA {
			if (g.Rect.CX == 0) != (g.Rect.CY == 0) {
				straight[g.ID] = true
			}
		}
		for i := range specB {
			if rebuilt[specA[i].ID] {
				continue
			}
			if straight[specA[i].ID] && specA[i].ID == specB[i].ID {
				if d := max(abs64(specA[i].Rect.X-specB[i].Rect.X), abs64(specA[i].Rect.Y-specB[i].Rect.Y),
					abs64(specA[i].Rect.CX-specB[i].Rect.CX), abs64(specA[i].Rect.CY-specB[i].Rect.CY)); d <= StraightenPx*EMUPerPx {
					continue
				}
			}
			for _, d := range []int64{
				specA[i].Rect.X - specB[i].Rect.X,
				specA[i].Rect.Y - specB[i].Rect.Y,
				specA[i].Rect.CX - specB[i].Rect.CX,
				specA[i].Rect.CY - specB[i].Rect.CY,
			} {
				if d < 0 {
					d = -d
				}
				if d > res.GeomDriftEMU {
					res.GeomDriftEMU = d
				}
			}
		}
		// Text, shape by shape. A rebuilt overlay carries its base's whole line
		// with only its own words in ink; its own words must all still be there.
		if ok, err := sameTexts(docB, docA, rebuilt); err != nil {
			return nil, err
		} else if !ok {
			res.TextDiffs = append(res.TextDiffs, name)
		}
		for _, g := range Groups(docA) {
			if !g.Idt {
				res.GroupsLeft++
			}
		}
		ign := drawable(Geoms(docA, IgnoreChSpace))
		nos := drawable(Geoms(docA, NoScale))
		for i := range specB {
			if i >= len(ign) || i >= len(nos) {
				break
			}
			if rebuilt[specA[i].ID] || straight[specA[i].ID] {
				continue
			}
			di := math.Max(
				math.Abs(float64(ign[i].Rect.X-specB[i].Rect.X)),
				math.Abs(float64(ign[i].Rect.Y-specB[i].Rect.Y)))
			res.ResidualIgn = math.Max(res.ResidualIgn, di/EMUPerPt)
			dn := math.Max(
				math.Abs(float64(nos[i].Rect.X-specB[i].Rect.X)),
				math.Abs(float64(nos[i].Rect.Y-specB[i].Rect.Y)))
			res.ResidualNoSc = math.Max(res.ResidualNoSc, dn/EMUPerPt)
		}
	}
	return res, nil
}

// drawable drops the group containers, so an ungrouped deck can be compared
// with the deck it came from: only real objects must keep their rectangles.
func drawable(gs []Geom) []Geom {
	out := gs[:0]
	for _, g := range gs {
		if g.Kind != "p:grpSp" {
			out = append(out, g)
		}
	}
	return out
}

// partNumber is the trailing number of a part name: ppt/slides/slide12.xml is
// 12, and Slide_N order in an enbx follows the same page order.
func partNumber(name string) int {
	base := name
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		base = name[i+1:]
	}
	digits := 0
	for _, r := range base {
		if r >= '0' && r <= '9' {
			digits = digits*10 + int(r-'0')
		}
	}
	return digits
}

// naturalLess orders parts by their number, then by name.
func naturalLess(a, b string) bool {
	na, nb := partNumber(a), partNumber(b)
	if na != nb {
		return na < nb
	}
	return a < b
}

// sameTexts compares the text of every top-level shape of two scans of a part.
func sameTexts(before, after *Doc, rebuilt map[string]bool) (bool, error) {
	textOf := func(d *Doc) (map[string]string, []string, error) {
		m := map[string]string{}
		var order []string
		for _, s := range d.Roots {
			ts, err := Texts(d.Data[s.TagStart:s.EndEnd])
			if err != nil {
				return nil, nil, err
			}
			key := s.ID + "@" + s.Kind
			m[key] = strings.Join(ts, "")
			order = append(order, key)
		}
		return m, order, nil
	}
	mb, ob, err := textOf(before)
	if err != nil {
		return false, err
	}
	ma, oa, err := textOf(after)
	if err != nil {
		return false, err
	}
	if strings.Join(ob, ",") != strings.Join(oa, ",") {
		// The shape tree changed (ungrouping): compare the whole run sequence.
		tb, err := Texts(before.Data)
		if err != nil {
			return false, err
		}
		ta, err := Texts(after.Data)
		if err != nil {
			return false, err
		}
		return strings.Join(tb, "\x00") == strings.Join(ta, "\x00"), nil
	}
	for k, tb := range mb {
		ta := ma[k]
		id := k[:strings.IndexByte(k, '@')]
		if rebuilt[id] {
			if !strings.Contains(ta, tb) {
				return false, nil
			}
			continue
		}
		if ta != tb {
			return false, nil
		}
	}
	return true, nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
