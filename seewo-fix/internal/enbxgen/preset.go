// Package enbxgen converts a PowerPoint deck straight into 希沃白板's own
// courseware format (.enbx), so that a deck can be opened in 希沃 without
// going through its cloud PPTX converter.
//
// The format was checked against a sample exported by 希沃's converter.
// Automated tests use synthetic decks; visual verification uses local
// PowerPoint and 希沃 screenshots. This package also restores supported
// details such as text highlights and line breaks lost by the cloud importer.
package enbxgen

import (
	_ "embed"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// presetShapeDefinitions.xml is the DrawingML preset geometry table
// (ECMA-376 part 1, annex D), as shipped with Apache POI under the Apache
// License 2.0.
//
//go:embed presetShapeDefinitions.xml
var presetXML []byte

type gd struct {
	Name string `xml:"name,attr"`
	Fmla string `xml:"fmla,attr"`
}

type pathCmd struct {
	XMLName xml.Name
	Pts     []struct {
		X string `xml:"x,attr"`
		Y string `xml:"y,attr"`
	} `xml:"pt"`
	WR    string `xml:"wR,attr"`
	HR    string `xml:"hR,attr"`
	StAng string `xml:"stAng,attr"`
	SwAng string `xml:"swAng,attr"`
}

type geomPath struct {
	W      string    `xml:"w,attr"`
	H      string    `xml:"h,attr"`
	Fill   string    `xml:"fill,attr"`
	Stroke string    `xml:"stroke,attr"`
	Cmds   []pathCmd `xml:",any"`
}

type presetDef struct {
	Av    []gd       `xml:"avLst>gd"`
	Gd    []gd       `xml:"gdLst>gd"`
	Paths []geomPath `xml:"pathLst>path"`
}

var (
	presetOnce sync.Once
	presets    map[string]*presetDef
	presetErr  error
)

func loadPresets() {
	presets = map[string]*presetDef{}
	dec := xml.NewDecoder(strings.NewReader(string(presetXML)))
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				var d presetDef
				if err := dec.DecodeElement(&d, &t); err != nil {
					presetErr = err
					return
				}
				presets[t.Name.Local] = &d
				depth--
			}
		case xml.EndElement:
			depth--
		}
	}
}

// ShapePath is one sub-path of a shape as 希沃 stores it.
type ShapePath struct {
	Path     string
	FillMode string // Norm, None, DarkenLess, …
	IsFilled bool
	IsStroke bool
}

// HasPreset reports whether a DrawingML preset geometry is known.
func HasPreset(prst string) bool {
	presetOnce.Do(loadPresets)
	_, ok := presets[prst]
	return ok
}

// angle units: 60000ths of a degree
func rad(a float64) float64 { return a / 60000 * math.Pi / 180 }

type env map[string]float64

func (e env) val(s string) (float64, error) {
	if v, ok := e[s]; ok {
		return v, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("unknown guide %q", s)
	}
	return v, nil
}

func (e env) eval(f string) (float64, error) {
	p := strings.Fields(f)
	if len(p) == 0 {
		return 0, fmt.Errorf("empty formula")
	}
	a := make([]float64, len(p)-1)
	for i, s := range p[1:] {
		v, err := e.val(s)
		if err != nil {
			return 0, err
		}
		a[i] = v
	}
	need := func(n int) error {
		if len(a) < n {
			return fmt.Errorf("formula %q needs %d arguments", f, n)
		}
		return nil
	}
	switch p[0] {
	case "val":
		return a[0], need(1)
	case "*/":
		if err := need(3); err != nil {
			return 0, err
		}
		if a[2] == 0 {
			return 0, nil
		}
		return a[0] * a[1] / a[2], nil
	case "+-":
		return a[0] + a[1] - a[2], need(3)
	case "+/":
		if err := need(3); err != nil {
			return 0, err
		}
		if a[2] == 0 {
			return 0, nil
		}
		return (a[0] + a[1]) / a[2], nil
	case "?:":
		if err := need(3); err != nil {
			return 0, err
		}
		if a[0] > 0 {
			return a[1], nil
		}
		return a[2], nil
	case "abs":
		return math.Abs(a[0]), need(1)
	case "at2":
		return math.Atan2(a[1], a[0]) * 180 / math.Pi * 60000, need(2)
	case "cat2":
		return a[0] * math.Cos(math.Atan2(a[2], a[1])), need(3)
	case "sat2":
		return a[0] * math.Sin(math.Atan2(a[2], a[1])), need(3)
	case "cos":
		return a[0] * math.Cos(rad(a[1])), need(2)
	case "sin":
		return a[0] * math.Sin(rad(a[1])), need(2)
	case "tan":
		return a[0] * math.Tan(rad(a[1])), need(2)
	case "max":
		return math.Max(a[0], a[1]), need(2)
	case "min":
		return math.Min(a[0], a[1]), need(2)
	case "mod":
		return math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2]), need(3)
	case "pin":
		if err := need(3); err != nil {
			return 0, err
		}
		return math.Max(a[0], math.Min(a[1], a[2])), nil
	case "sqrt":
		return math.Sqrt(math.Max(0, a[0])), need(1)
	}
	return 0, fmt.Errorf("unknown formula %q", f)
}

func builtins(w, h float64) env {
	e := env{"w": w, "h": h, "l": 0, "t": 0, "r": w, "b": h,
		"hc": w / 2, "vc": h / 2, "ss": math.Min(w, h), "ls": math.Max(w, h),
		"cd2": 10800000, "cd4": 5400000, "cd8": 2700000,
		"3cd4": 16200000, "3cd8": 8100000, "5cd8": 13500000, "7cd8": 18900000}
	for _, n := range []int{2, 3, 4, 5, 6, 8, 10, 12, 16, 32} {
		s := strconv.Itoa(n)
		e["wd"+s] = w / float64(n)
		e["hd"+s] = h / float64(n)
		e["ssd"+s] = e["ss"] / float64(n)
	}
	return e
}

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	if s == "-0.000" {
		return "0.000"
	}
	return s
}

// PresetPaths computes a preset geometry at w×h px, with adjust values from
// the shape's avLst overriding the defaults.
func PresetPaths(prst string, w, h float64, adjust map[string]float64) ([]ShapePath, error) {
	presetOnce.Do(loadPresets)
	if presetErr != nil {
		return nil, presetErr
	}
	d, ok := presets[prst]
	if !ok {
		return nil, fmt.Errorf("unknown preset geometry %q", prst)
	}
	e := builtins(w, h)
	for _, g := range d.Av {
		if v, ok := adjust[g.Name]; ok {
			e[g.Name] = v
			continue
		}
		v, err := e.eval(g.Fmla)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", prst, err)
		}
		e[g.Name] = v
	}
	for _, g := range d.Gd {
		v, err := e.eval(g.Fmla)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", prst, err)
		}
		e[g.Name] = v
	}
	var out []ShapePath
	for _, p := range d.Paths {
		sx, sy := 1.0, 1.0
		if p.W != "" {
			if pw, _ := strconv.ParseFloat(p.W, 64); pw > 0 {
				sx = w / pw
			}
		}
		if p.H != "" {
			if ph, _ := strconv.ParseFloat(p.H, 64); ph > 0 {
				sy = h / ph
			}
		}
		var b strings.Builder
		var cx, cy float64
		pt := func(x, y string) (float64, float64, error) {
			xv, err := e.val(x)
			if err != nil {
				return 0, 0, err
			}
			yv, err := e.val(y)
			if err != nil {
				return 0, 0, err
			}
			return xv * sx, yv * sy, nil
		}
		for _, c := range p.Cmds {
			switch c.XMLName.Local {
			case "moveTo", "lnTo":
				x, y, err := pt(c.Pts[0].X, c.Pts[0].Y)
				if err != nil {
					return nil, err
				}
				cmd := "M "
				if c.XMLName.Local == "lnTo" {
					cmd = "L "
				}
				b.WriteString(cmd + num(x) + "," + num(y) + " ")
				cx, cy = x, y
			case "cubicBezTo", "quadBezTo":
				cmd := "C "
				if c.XMLName.Local == "quadBezTo" {
					cmd = "Q "
				}
				b.WriteString(cmd)
				for i, q := range c.Pts {
					x, y, err := pt(q.X, q.Y)
					if err != nil {
						return nil, err
					}
					if i > 0 {
						b.WriteString(" ")
					}
					b.WriteString(num(x) + "," + num(y))
					cx, cy = x, y
				}
				b.WriteString(" ")
			case "arcTo":
				wr, err := e.val(c.WR)
				if err != nil {
					return nil, err
				}
				hr, err := e.val(c.HR)
				if err != nil {
					return nil, err
				}
				st, err := e.val(c.StAng)
				if err != nil {
					return nil, err
				}
				sw, err := e.val(c.SwAng)
				if err != nil {
					return nil, err
				}
				wr, hr = wr*sx, hr*sy
				// DrawingML angles are visual angles on the ellipse.
				param := func(a float64) float64 {
					r := rad(a)
					return math.Atan2(wr*math.Sin(r), hr*math.Cos(r))
				}
				t0 := param(st)
				t1 := param(st + sw)
				ocx, ocy := cx-wr*math.Cos(t0), cy-hr*math.Sin(t0)
				ex, ey := ocx+wr*math.Cos(t1), ocy+hr*math.Sin(t1)
				large, sweep := 0, 0
				if math.Abs(sw) > 10800000 {
					large = 1
				}
				if sw > 0 {
					sweep = 1
				}
				if math.Abs(sw) >= 21600000 {
					// a full turn: two halves
					hx, hy := ocx+wr*math.Cos(t0+math.Pi), ocy+hr*math.Sin(t0+math.Pi)
					fmt.Fprintf(&b, "A%s,%s,0,0,%d,%s,%s ", num(wr), num(hr), sweep, num(hx), num(hy))
				}
				fmt.Fprintf(&b, "A%s,%s,0,%d,%d,%s,%s ", num(wr), num(hr), large, sweep, num(ex), num(ey))
				cx, cy = ex, ey
			case "close":
				b.WriteString("z ")
			}
		}
		sp := ShapePath{Path: strings.TrimRight(b.String(), " ") + " ", IsFilled: p.Fill != "none", IsStroke: p.Stroke != "false"}
		switch p.Fill {
		case "", "norm":
			sp.FillMode = "Norm"
		default:
			r := []rune(p.Fill)
			r[0] = unicode.ToUpper(r[0])
			sp.FillMode = string(r)
		}
		out = append(out, sp)
	}
	return out, nil
}

// GeometryType is 希沃's name for a preset: its own Rectangle, else
// Custom_PPTX_<Preset>.
func GeometryType(prst string) string {
	if prst == "rect" {
		return "Rectangle"
	}
	r := []rune(prst)
	r[0] = unicode.ToUpper(r[0])
	return "Custom_PPTX_" + string(r)
}
