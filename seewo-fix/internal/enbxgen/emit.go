package enbxgen

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// w writes 希沃's indented XML: two spaces a level.
type w struct {
	b     strings.Builder
	depth int
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;").Replace(s)
}

func (o *w) open(tag string) {
	o.b.WriteString(strings.Repeat("  ", o.depth) + "<" + tag + ">\n")
	o.depth++
}

func (o *w) close(tag string) {
	o.depth--
	o.b.WriteString(strings.Repeat("  ", o.depth) + "</" + tag + ">\n")
}

func (o *w) leaf(tag, val string) {
	if val == "" {
		o.b.WriteString(strings.Repeat("  ", o.depth) + "<" + tag + " />\n")
		return
	}
	o.b.WriteString(strings.Repeat("  ", o.depth) + "<" + tag + ">" + esc(val) + "</" + tag + ">\n")
}

func f(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func boolStr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func (o *w) brush(tag, color string) {
	o.open(tag)
	o.leaf("ColorBrush", color)
	o.close(tag)
}

func (o *w) runProps(r run, withText bool) {
	o.open("TextRun")
	if withText {
		o.leaf("Text", r.text)
	}
	o.leaf("FontSize", f(r.sizePt*4/3))
	switch {
	case r.baseline > 0:
		o.leaf("FontVariants", "Superscript")
	case r.baseline < 0:
		o.leaf("FontVariants", "Subscript")
	default:
		o.leaf("FontVariants", "Normal")
	}
	if r.italic {
		o.leaf("FontStyle", "Italic")
	} else {
		o.leaf("FontStyle", "Normal")
	}
	if r.bold {
		o.leaf("FontWeight", "Bold")
	} else {
		o.leaf("FontWeight", "Normal")
	}
	o.open("FontFamily")
	o.leaf("Source", r.font)
	o.close("FontFamily")
	bg := "#00FFFFFF"
	if r.highlight != "" {
		bg = r.highlight
	}
	o.brush("Background", bg)
	o.brush("Foreground", r.color)
	o.leaf("Opacity", "1")
	if r.underline {
		o.leaf("Decoration", "Underline")
	}
	o.close("TextRun")
}

// richText writes a RichText block.
func (c *conv) richText(o *w, b body, sizeToContent string) {
	o.open("RichText")
	o.leaf("SizeToContent", sizeToContent)
	o.leaf("ArrangingType", "Horizontal")
	switch b.anchor {
	case "ctr":
		o.leaf("VerticalTextAlignment", "Center")
	case "b":
		o.leaf("VerticalTextAlignment", "Bottom")
	default:
		o.leaf("VerticalTextAlignment", "Top")
	}
	o.open("TextLines")
	var all strings.Builder
	for _, p := range b.paras {
		o.open("TextLine")
		font := p.def.font
		ls, ok := lineSpacing(font, p.lnPct)
		if !ok {
			ls, _ = lineSpacing("Times New Roman", p.lnPct)
			if len(p.runs) > 0 {
				c.warn("字体「%s」的行距规则未知，按 Times New Roman 计算", font)
			}
		}
		// The Mac renderer drops FixedLineSpacing when cloning paragraph
		// properties. Express the desired height through LineSpacing,
		// including the baseline mismatch in mixed Latin/Kaiti SC lines.
		mappedKaiti := c.mapFont(p.def).font == "Kaiti SC"
		for _, r := range p.runs {
			mappedKaiti = mappedKaiti || c.mapFont(r).font == "Kaiti SC"
		}
		if mappedKaiti || p.lnPts > 0 {
			ls = c.renderLineSpacing(p)
		}
		o.leaf("LineSpacing", f(ls))
		o.leaf("FixedLineSpacing", "NaN")
		o.leaf("TextAlignment", p.align)
		o.open("TextRuns")
		for _, r := range p.runs {
			o.runProps(c.mapFont(r), true)
			all.WriteString(r.text)
		}
		o.close("TextRuns")
		o.open("DefaultRunProperty")
		o.runProps(c.mapFont(p.def), false)
		o.close("DefaultRunProperty")
		o.leaf("MarginLeft", f(p.marL))
		if p.bullet {
			o.leaf("Indent", f(math.Abs(p.indent)))
		} else {
			o.leaf("Indent", f(p.indent))
		}
		o.open("TextMarkerStyle")
		if p.bullet {
			bf := p.buFont
			if bf == "" {
				bf = p.def.font
			}
			o.open("FontFamily")
			o.leaf("Source", bf)
			o.close("FontFamily")
			o.leaf("FontSize", f(p.def.sizePt*4/3))
			o.brush("Foreground", p.def.color)
			o.leaf("Char", p.buChar)
		} else {
			o.leaf("FontSize", "0")
		}
		o.close("TextMarkerStyle")
		o.leaf("SpaceBefore", f(p.spcBef))
		o.leaf("SpaceAfter", f(p.spcAft))
		o.open("Lines")
		for _, l := range p.lines {
			o.open("LineProperty")
			o.leaf("StartOffset", strconv.Itoa(l.start))
			o.leaf("Length", strconv.Itoa(l.length))
			o.close("LineProperty")
		}
		o.close("Lines")
		o.leaf("Direction", "LeftToRight")
		o.leaf("IndentLevel", "0")
		if p.bullet {
			o.leaf("TextMarker", "Circle")
			o.leaf("IndentType", "Hanging")
		} else {
			o.leaf("TextMarker", "None")
			o.leaf("IndentType", "FirstLine")
		}
		o.close("TextLine")
		if p.warning != "" {
			c.warn("%s未转换", p.warning)
		}
	}
	o.close("TextLines")
	o.leaf("Text", all.String())
	o.close("RichText")
}

func (c *conv) mapFont(r run) run {
	if to, ok := c.opt.FontMap[r.font]; ok {
		r.font = to
	}
	return r
}

// common writes Id … HasMask and the origin, the tail every element shares.
type origin struct {
	id, name string
	animKey  string // animations to attach, when not the shape's own
}

// extra writes what goes between the mirror and the origin (a picture's clip).
func (c *conv) tail(o *w, id string, x, y, wd, ht, rot float64, org *origin, mirror string, extra func(*w)) {
	o.leaf("Id", id)
	o.leaf("X", f(x))
	o.leaf("Y", f(y))
	o.leaf("Width", f(wd))
	o.leaf("Height", f(ht))
	o.leaf("Rotation", f(rot))
	o.leaf("IsLocked", "False")
	o.leaf("CanClone", "False")
	if org != nil {
		key := org.id
		if org.animKey != "" {
			key = org.animKey
		}
		c.animations(o, key)
	}
	o.leaf("HasMask", "False")
	if mirror != "" {
		o.open("PptElement")
		o.leaf("MirrorType", mirror)
		o.close("PptElement")
	}
	if extra != nil {
		extra(o)
	}
	if org != nil {
		o.open("ElementPptxOriginId")
		o.leaf("OriginPptxId", org.id)
		o.leaf("OriginPptxName", org.name)
		o.close("ElementPptxOriginId")
		o.open("PptxToEnbxElementAddition")
		o.leaf("ElementPlacement", "Slide")
		o.close("PptxToEnbxElementAddition")
	}
}

type anim struct {
	typ, id, category, trigger, direction, effect string
	number                                        int
	duration, delay                               int64
	accel, decel                                  float64
	path                                          string // motion path, px
}

func (c *conv) animations(o *w, spid string) {
	as := c.anims[spid]
	if len(as) == 0 {
		return
	}
	o.open("Animations")
	for _, a := range as {
		o.open("Animation")
		o.leaf("Type", a.typ)
		o.leaf("Id", a.id)
		o.leaf("Number", strconv.Itoa(a.number))
		o.leaf("Category", a.category)
		o.leaf("Trigger", a.trigger)
		o.leaf("Duration", strconv.FormatInt(a.duration, 10))
		o.leaf("Delay", strconv.FormatInt(a.delay, 10))
		o.leaf("AccelerationRatio", f(a.accel))
		o.leaf("DecelerationRatio", f(a.decel))
		o.leaf("RepeatBehavior", "0")
		if a.path != "" {
			o.leaf("Path", a.path)
		}
		if a.direction != "" {
			o.leaf("Direction", a.direction)
		}
		if a.effect != "" {
			o.leaf("Effect", a.effect)
		}
		o.open("ElementAnimationExtension")
		o.leaf("RepeatCount", "1")
		o.close("ElementAnimationExtension")
		o.close("Animation")
	}
	o.close("Animations")
}

// shapeStyle is what a Shape element says about fill and outline.
type shapeStyle struct {
	fill, stroke string // #AARRGGBB
	thickness    float64
	hasLine      bool
	cap          string // Flat, Round, Square
	headEnd      string
	tailEnd      string
	dash         string
}

func (c *conv) shape(o *w, id string, prst string, adj map[string]float64, x, y, wd, ht, rot float64,
	st shapeStyle, org *origin, mirror string) error {
	o.open("Shape")
	o.open("Geometry")
	o.open("PresetGeometry")
	o.leaf("GeometryType", GeometryType(prst))
	o.leaf("Adjusts", "")
	o.close("PresetGeometry")
	o.close("Geometry")
	pw, ph := wd, ht
	paths, err := PresetPaths(prst, pw, ph, adj)
	if err != nil {
		return err
	}
	if prst == "line" || prst == "straightConnector1" {
		// a level line is drawn in a box as high as its stroke
		if ht == 0 {
			ht = st.thickness
		}
	}
	all := ""
	for _, p := range paths {
		all += p.Path
	}
	if prst == "rect" {
		o.leaf("Path", fmt.Sprintf("M 0.000,0.000 L %s,0.000 L %s,%s L 0.000,%s z", num(wd), num(wd), num(ht), num(ht)))
	} else {
		o.leaf("Path", all)
		o.open("ShapePaths")
		for _, p := range paths {
			o.open("ShapePath")
			o.leaf("Path", p.Path)
			o.leaf("FillMode", p.FillMode)
			o.leaf("IsFilled", boolStr(p.IsFilled))
			o.leaf("IsStroke", boolStr(p.IsStroke))
			o.close("ShapePath")
		}
		o.close("ShapePaths")
	}
	o.brush("Background", st.fill)
	o.brush("Foreground", st.stroke)
	o.leaf("Thickness", f(st.thickness))
	if st.thickness > 0 && arrowsOf(st) {
		o.leaf("LineType", "DoubleArrow")
	} else if st.thickness > 0 {
		o.leaf("LineType", "Normal")
	} else {
		o.leaf("LineType", "None")
	}
	o.leaf("Opacity", "1")
	connector := prst == "line" || strings.HasPrefix(prst, "straightConnector") || strings.HasPrefix(prst, "bentConnector") || strings.HasPrefix(prst, "curvedConnector")
	closed := !connector && prst != "rect"
	arrows := st.headEnd != "" || st.tailEnd != ""
	if st.thickness > 0 && (closed || arrows) {
		o.open("Line")
		for _, e := range []struct{ tag, typ string }{{"HeadEnd", st.headEnd}, {"TailEnd", st.tailEnd}} {
			t := e.typ
			if t == "" {
				if connector {
					continue // a line only lists the ends it decorates
				}
				t = "None"
			}
			o.open(e.tag)
			o.leaf("Type", t)
			o.leaf("ArrowType", t)
			o.leaf("Width", "Medium")
			o.leaf("Length", "Medium")
			o.close(e.tag)
		}
		o.close("Line")
	}
	if connector {
		o.leaf("TextWidth", "0")
		o.leaf("TextMargin", "0,0,0,0")
		o.leaf("IsTextLocationCustomize", "False")
	} else {
		o.leaf("TextWidth", f(wd))
		o.leaf("TextMargin", "9.600000000000001,4.800000000000001,9.600000000000001,4.800000000000001")
		o.leaf("IsTextLocationCustomize", boolStr(prst != "upArrow" && prst != "triangle"))
	}
	if st.thickness > 0 {
		o.open("PenSetting")
		cp := st.cap
		if cp == "" {
			cp = "Flat"
		}
		o.leaf("LineCap", cp)
		o.open("DashStyle")
		o.leaf("Offset", "0")
		o.leaf("Dashes", st.dash)
		o.close("DashStyle")
		o.leaf("IsArrowDash", "False")
		o.close("PenSetting")
	}
	c.tail(o, id, x, y, wd, ht, rot, org, mirror, nil)
	o.close("Shape")
	return nil
}

func arrowsOf(st shapeStyle) bool { return st.headEnd != "" || st.tailEnd != "" }

// arrowType is 希沃's name for a DrawingML line end.
func arrowType(t string) string {
	switch t {
	case "arrow":
		return "Arrow"
	case "triangle":
		return "Triangle"
	case "stealth":
		return "Stealth"
	case "diamond":
		return "Diamond"
	case "oval":
		return "Oval"
	}
	return ""
}
