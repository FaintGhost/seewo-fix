package enbxgen

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"

	"seewo-fix/internal/pptx"
)

// SaveInfoMetadataFile.xml is 希沃's declaration of the element types a
// courseware file uses, as its converter writes it.
//
//go:embed SaveInfoMetadataFile.xml
var saveInfo []byte

// Options selects what the converter writes beyond a faithful conversion.
type Options struct {
	// Name is the courseware name 希沃 shows.
	Name string
	// Highlights keeps <a:highlight> as the runs' background.
	Highlights bool
	// FontMap renames fonts, e.g. 楷体 → Kaiti SC on a Mac.
	FontMap map[string]string
	// Measurer measures text for line breaking; pptx.SystemFonts by default.
	Measurer pptx.Measurer
	// Pages limits the conversion to these 1-based pages; nil means all.
	Pages []int
	// Now stamps the document; zero means time.Now().
	Now time.Time
}

// Result is a converted deck.
type Result struct {
	Enbx     []byte
	Warnings []string
}

type resource struct {
	id, ext string
	data    []byte
}

type conv struct {
	d       *deck
	opt     Options
	m       pptx.Measurer
	warns   []string
	seen    map[string]bool
	anims   map[string][]anim
	order   []string
	slideNo int
	seed    string
	res     map[string]resource
	resList []string
	n       int
	// cellInsets, when set, replaces a body's own insets (table cells
	// take theirs from a:tcPr)
	cellInsets *[4]int64
	// paraKeys lists, per shape id, the paragraph ranges animated on their own
	paraKeys map[string][]string
}

func (c *conv) warn(format string, a ...any) {
	s := fmt.Sprintf("第 %d 页：", c.slideNo) + fmt.Sprintf(format, a...)
	if !c.seen[s] {
		c.seen[s] = true
		c.warns = append(c.warns, s)
	}
}

func (c *conv) warnFont(font string) {
	c.warn("没有字体「%s」，换行按估算字宽计算", font)
}

// newID is a stable 32-hex id: the same deck converts to the same file.
func (c *conv) newID(kind string) string {
	c.n++
	sum := md5.Sum([]byte(fmt.Sprintf("%s|%d|%s|%d", c.seed, c.slideNo, kind, c.n)))
	return hex.EncodeToString(sum[:])
}

// Convert turns a .pptx into an .enbx.
func Convert(data []byte, opt Options) (*Result, error) {
	d, err := readDeck(data)
	if err != nil {
		return nil, err
	}
	c := &conv{d: d, opt: opt, m: opt.Measurer, seen: map[string]bool{}, res: map[string]resource{}}
	if c.m == nil {
		c.m = pptx.SystemFonts
	}
	sum := md5.Sum(data)
	c.seed = hex.EncodeToString(sum[:])
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	want := map[int]bool{}
	for _, p := range opt.Pages {
		want[p] = true
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// 希沃's Mac importer requires explicit parents when extracting lazy
	// resources; standard ZIP readers accepting implicit directories hid
	// this failure in tests. Emit the folders before any of their files.
	for _, name := range []string{"Slides/", "Resources/"} {
		header := &zip.FileHeader{Name: name, Method: zip.Store, Modified: opt.Now}
		header.SetMode(fs.ModeDir | 0o755)
		if _, err := zw.CreateHeader(header); err != nil {
			return nil, err
		}
	}
	put := func(name string, body []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: opt.Now})
		if err != nil {
			return err
		}
		_, err = w.Write(body)
		return err
	}
	bom := "\xef\xbb\xbf"
	var slideIDs []string
	for i, s := range d.slides {
		if len(want) > 0 && !want[i+1] {
			continue
		}
		c.slideNo = i + 1
		id, body, err := c.slide(s)
		if err != nil {
			return nil, fmt.Errorf("第 %d 页: %w", i+1, err)
		}
		if err := put(fmt.Sprintf("Slides/Slide_%d.xml", len(slideIDs)), []byte(bom+body)); err != nil {
			return nil, err
		}
		slideIDs = append(slideIDs, id)
	}
	wpx, hpx := px(d.cx), px(d.cy)
	var o w
	o.open("Document")
	name := opt.Name
	if name == "" {
		name = "课件"
	}
	o.leaf("Name", name)
	o.leaf("Creator", "Cyclone2.0")
	o.leaf("LastModifiedBy", "Cyclone2.0")
	stamp := opt.Now.UTC().Format("01/02/2006 15:04:05")
	o.leaf("CreatedDateTime", stamp)
	o.leaf("ModifiedDateTime", stamp)
	o.leaf("CreatedDocumentVersion", "1.0")
	o.leaf("DocumentVersion", "1.0")
	o.leaf("CreatedAppVersion", "EN-PPT-5.1.19.9")
	o.leaf("AppVersion", "EN-PPT-5.1.19.9")
	o.close("Document")
	if err := put("Document.xml", []byte(bom+o.b.String())); err != nil {
		return nil, err
	}
	if err := put("SaveInfoMetadataFile.xml", saveInfo); err != nil {
		return nil, err
	}
	var bo w
	bo.open("Board")
	bo.leaf("SlideWidth", f(wpx))
	bo.leaf("SlideHeight", f(hpx))
	bo.open("Slides")
	for _, id := range slideIDs {
		bo.leaf("Item", id)
	}
	bo.close("Slides")
	bo.open("ThemeForBoard")
	bo.leaf("ThemeId", "-1")
	bo.close("ThemeForBoard")
	bo.close("Board")
	if err := put("Board.xml", []byte(bom+bo.b.String())); err != nil {
		return nil, err
	}
	var ro w
	ro.open("Reference")
	ro.open("Relationships")
	for _, k := range c.resList {
		r := c.res[k]
		ro.open("Relationship")
		ro.leaf("Id", r.id)
		ro.leaf("Target", "Resources/"+r.id+r.ext)
		ro.leaf("Hash", r.id)
		ro.close("Relationship")
	}
	ro.close("Relationships")
	ro.close("Reference")
	if err := put("Reference.xml", []byte(bom+ro.b.String())); err != nil {
		return nil, err
	}
	for _, k := range c.resList {
		r := c.res[k]
		if err := put("Resources/"+r.id+r.ext, r.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &Result{Enbx: buf.Bytes(), Warnings: c.warns}, nil
}

// timing reads a slide's animation sequence.
func (c *conv) timing(root *etree.Element) {
	c.anims = map[string][]anim{}
	c.paraKeys = map[string][]string{}
	c.order = nil
	t := root.FindElement("./timing")
	if t == nil {
		return
	}
	n := 0
	var walk func(e *etree.Element)
	walk = func(e *etree.Element) {
		for _, ch := range e.ChildElements() {
			if ch.Tag == "cTn" && ch.SelectAttr("presetClass") != nil {
				c.effect(ch, &n)
				continue
			}
			walk(ch)
		}
	}
	walk(t)
}

func (c *conv) effect(ctn *etree.Element, n *int) {
	tgt := ctn.FindElement(".//spTgt")
	if tgt == nil {
		return
	}
	spid := tgt.SelectAttrValue("spid", "")
	key := spid
	if pr := tgt.FindElement("./txEl/pRg"); pr != nil {
		// a paragraph build: animated on a copy showing just these paragraphs
		key = fmt.Sprintf("%s#%d-%d", spid, attrInt(pr, "st"), attrInt(pr, "end"))
		if c.paraKeys[spid] == nil || !contains(c.paraKeys[spid], key) {
			c.paraKeys[spid] = append(c.paraKeys[spid], key)
		}
	}
	cls := ctn.SelectAttrValue("presetClass", "")
	pid := ctn.SelectAttrValue("presetID", "")
	sub := ctn.SelectAttrValue("presetSubtype", "0")
	durMs := int64(0)
	for _, e := range ctn.FindElements(".//cTn") {
		if v, err := strconv.ParseInt(e.SelectAttrValue("dur", ""), 10, 64); err == nil && v > durMs {
			durMs = v
		}
	}
	a := anim{trigger: "Click"}
	switch ctn.SelectAttrValue("nodeType", "") {
	case "withEffect":
		a.trigger = "Before"
	case "afterEffect":
		a.trigger = "After"
	}
	switch {
	case cls == "entr" && pid == "3":
		a.typ, a.category, a.effect = "BlindIn", "Appearance", "In"
		a.direction = "Horizontal"
		if sub == "5" {
			a.direction = "Vertical"
		}
		a.duration = durMs * 10000
	case cls == "entr" && pid == "1":
		a.typ, a.category, a.duration = "FadeIn", "Appearance", 10000
	case cls == "entr" && pid == "10":
		a.typ, a.category, a.duration = "FadeIn", "Appearance", durMs*10000
	case cls == "exit" && pid == "1":
		a.typ, a.category, a.duration = "FadeOut", "Disappearance", 10000
	case cls == "exit" && (pid == "3" || pid == "10"):
		a.typ, a.category, a.duration = "FadeOut", "Disappearance", durMs*10000
	case cls == "path":
		mp := ctn.FindElement(".//animMotion")
		if mp == nil {
			c.warn("路径动画没有路径，已跳过")
			return
		}
		path, ok := c.motionPath(mp.SelectAttrValue("path", ""))
		if !ok {
			c.warn("路径动画的路径无法识别，已跳过")
			return
		}
		a.typ, a.category, a.path = "Curve", "Emphasis", path
		a.duration = durMs * 10000
		a.accel = float64(attrInt(ctn, "accel")) / 100000
		a.decel = float64(attrInt(ctn, "decel")) / 100000
	default:
		c.warn("动画（%s 预设 %s）暂不支持，已跳过", cls, pid)
		return
	}
	*n++
	a.number = *n
	a.id = c.newID("anim")
	c.anims[key] = append(c.anims[key], a)
	c.order = append(c.order, a.id)
}

func (c *conv) slide(s slideRef) (string, string, error) {
	doc, err := c.d.xml(s.part)
	if err != nil {
		return "", "", err
	}
	root := doc.Root()
	rels := c.d.rels(s.part)
	c.timing(root)
	id := c.newID("slide")
	var o w
	o.open("Slide")
	o.leaf("Id", id)
	o.leaf("Width", f(px(c.d.cx)))
	o.leaf("Height", f(px(c.d.cy)))
	o.leaf("KeepIndependentSize", "False")
	bg := "#FFFFFFFF"
	if col, ok := c.d.theme.color(root.FindElement("./cSld/bg/bgPr/solidFill")); ok {
		bg = col
	} else if root.FindElement("./cSld/bg") != nil {
		c.warn("背景（图片/渐变/主题背景）未转换，按白色处理")
	}
	o.brush("Background", bg)
	o.open("Elements")
	tree := root.FindElement("./cSld/spTree")
	if tree != nil {
		for _, e := range tree.ChildElements() {
			if err := c.element(&o, e, rels, xform{1, 0, 1, 0}, 0, 0, true); err != nil {
				return "", "", err
			}
		}
	}
	o.close("Elements")
	if len(c.order) > 0 {
		o.open("AnimationOrders")
		for _, a := range c.order {
			o.leaf("Item", a)
		}
		o.close("AnimationOrders")
	}
	o.open("SlidePptxOriginIdExtension")
	o.leaf("OriginPptxId", s.id)
	o.close("SlidePptxOriginIdExtension")
	o.open("ThemeForSlide")
	o.leaf("ThemeId", "-1")
	o.close("ThemeForSlide")
	o.close("Slide")
	return id, o.b.String(), nil
}

// xform maps a group's child coordinates to page EMU.
type xform struct{ sx, tx, sy, ty float64 }

func (x xform) pt(ex, ey int64) (float64, float64) {
	return float64(ex)*x.sx + x.tx, float64(ey)*x.sy + x.ty
}

type frame struct {
	x, y, cx, cy float64 // page EMU
	rot          float64 // degrees
	flipH, flipV bool
}

func readFrame(xfrm *etree.Element, t xform) (frame, bool) {
	if xfrm == nil {
		return frame{}, false
	}
	off, ext := xfrm.FindElement("./off"), xfrm.FindElement("./ext")
	if off == nil || ext == nil {
		return frame{}, false
	}
	x, y := t.pt(attrInt(off, "x"), attrInt(off, "y"))
	fr := frame{x: x, y: y, cx: float64(attrInt(ext, "cx")) * t.sx, cy: float64(attrInt(ext, "cy")) * t.sy}
	fr.rot = float64(attrInt(xfrm, "rot")) / 60000
	fr.flipH = xfrm.SelectAttrValue("flipH", "0") == "1"
	fr.flipV = xfrm.SelectAttrValue("flipV", "0") == "1"
	return fr, true
}

func (fr frame) mirror() string {
	switch {
	case fr.flipH && fr.flipV:
		return "BothMirror"
	case fr.flipH:
		return "HorizontalMirror"
	case fr.flipV:
		return "VerticalMirror"
	}
	return ""
}

func nameOf(e *etree.Element) origin {
	nv := e.FindElement(".//cNvPr")
	if nv == nil {
		return origin{}
	}
	return origin{id: nv.SelectAttrValue("id", ""), name: nv.SelectAttrValue("name", "")}
}

// style resolves what a shape's spPr and p:style say about fill and outline.
func (c *conv) style(e *etree.Element) shapeStyle {
	st := shapeStyle{fill: "#00FFFFFF", stroke: "#00FFFFFF"}
	sppr := e.FindElement("./spPr")
	pst := e.FindElement("./style")
	fillSet := false
	if sppr != nil {
		for _, ch := range sppr.ChildElements() {
			switch ch.Tag {
			case "noFill":
				fillSet = true
			case "solidFill":
				fillSet = true
				st.fill, _ = c.d.theme.color(ch)
			case "gradFill":
				fillSet = true
				if gs := ch.FindElement("./gsLst/gs"); gs != nil {
					st.fill, _ = c.d.theme.color(gs)
				}
				c.warn("渐变填充按第一种颜色处理")
			case "blipFill", "pattFill":
				fillSet = true
				c.warn("图片/图案填充未转换")
			}
		}
	}
	if !fillSet && pst != nil {
		if fr := pst.FindElement("./fillRef"); fr != nil && attrInt(fr, "idx") > 0 {
			st.fill, _ = c.d.theme.color(fr)
		}
	}
	var ln *etree.Element
	if sppr != nil {
		ln = sppr.FindElement("./ln")
	}
	lnRef := (*etree.Element)(nil)
	if pst != nil {
		lnRef = pst.FindElement("./lnRef")
	}
	strokeSet := false
	if ln != nil {
		if ln.FindElement("./noFill") != nil {
			strokeSet = true
		} else if sf := ln.FindElement("./solidFill"); sf != nil {
			strokeSet = true
			st.stroke, _ = c.d.theme.color(sf)
			st.hasLine = true
		}
		if w := attrInt(ln, "w"); w > 0 {
			st.thickness = px(w)
		}
		switch ln.SelectAttrValue("cap", "") {
		case "rnd":
			st.cap = "Round"
		case "sq":
			st.cap = "Square"
		}
		if pd := ln.FindElement("./prstDash"); pd != nil && pd.SelectAttrValue("val", "solid") != "solid" {
			c.warn("虚线样式未转换")
		}
		for _, end := range []string{"headEnd", "tailEnd"} {
			if he := ln.FindElement("./" + end); he != nil && he.SelectAttrValue("type", "none") != "none" {
				t := arrowType(he.SelectAttrValue("type", ""))
				if t == "" {
					c.warn("线条箭头样式「%s」未转换", he.SelectAttrValue("type", ""))
					continue
				}
				if end == "headEnd" {
					st.headEnd = t
				} else {
					st.tailEnd = t
				}
			}
		}
	}
	if !strokeSet && lnRef != nil && attrInt(lnRef, "idx") > 0 {
		st.stroke, _ = c.d.theme.color(lnRef)
		st.hasLine = true
	}
	if st.hasLine && st.thickness == 0 {
		st.thickness = px(12700) // theme line style 1–3 of this deck: 1 pt
	}
	if !st.hasLine {
		st.thickness = 0
		st.stroke = "#00FFFFFF"
	}
	return st
}

func visible(color string) bool { return color != "" && !strings.HasPrefix(color, "#00") }

func adjusts(e *etree.Element) map[string]float64 {
	out := map[string]float64{}
	for _, g := range e.FindElements("./spPr/prstGeom/avLst/gd") {
		fm := strings.Fields(g.SelectAttrValue("fmla", ""))
		if len(fm) == 2 && fm[0] == "val" {
			v, _ := strconv.ParseFloat(fm[1], 64)
			out[g.SelectAttrValue("name", "")] = v
		}
	}
	return out
}

func hasText(tb *etree.Element) bool {
	if tb == nil {
		return false
	}
	for _, t := range tb.FindElements(".//t") {
		if strings.TrimSpace(t.Text()) != "" {
			return true
		}
	}
	return false
}

// element converts one child of an spTree; gx, gy is the page position of
// the enclosing group, whose children 希沃 places relative to it.
func (c *conv) element(o *w, e *etree.Element, rels map[string]string, t xform, gx, gy float64, top bool) error {
	org := nameOf(e)
	porg := &org
	switch e.Tag {
	case "sp", "cxnSp":
		fr, ok := readFrame(e.FindElement("./spPr/xfrm"), t)
		if !ok {
			if e.FindElement("./nvSpPr/nvPr/ph") != nil {
				c.warn("占位符「%s」未转换", org.name)
			}
			return nil
		}
		x, y, wd, ht := px(int64(fr.x))-gx, px(int64(fr.y))-gy, px(int64(fr.cx)), px(int64(fr.cy))
		prst := "rect"
		if g := e.FindElement("./spPr/prstGeom"); g != nil {
			prst = g.SelectAttrValue("prst", "rect")
		} else if e.FindElement("./spPr/custGeom") != nil {
			c.warn("自定义形状「%s」未转换", org.name)
			return nil
		}
		if !HasPreset(prst) {
			c.warn("形状「%s」(%s) 未转换", org.name, prst)
			return nil
		}
		st := c.style(e)
		tb := e.FindElement("./txBody")
		if !hasText(tb) {
			return c.shape(o, c.newID("shape"), prst, adjusts(e), x, y, wd, ht, fr.rot, st, porg, fr.mirror())
		}
		fontColor := ""
		if fr := e.FindElement("./style/fontRef"); fr != nil {
			fontColor, _ = c.d.theme.color(fr)
		}
		b := c.readBody(tb, int64(fr.cx), fontColor)
		box := visible(st.fill) || st.thickness > 0
		if prst != "rect" {
			box = true
		}
		if !box && len(c.paraKeys[org.id]) > 0 {
			c.paragraphBuild(o, b, x, y, wd, ht, fr.rot, org)
			return nil
		}
		if box && len(c.paraKeys[org.id]) > 0 {
			c.warn("带边框文本框「%s」的按段落动画按整个文本框处理", org.name)
			for _, k := range c.paraKeys[org.id] {
				c.anims[org.id] = append(c.anims[org.id], c.anims[k]...)
			}
		}
		if !box {
			band, ok := c.highlightBand(&b, int64(fr.cx))
			if !ok {
				band, ok = c.partialBands(&b, int64(fr.cx))
			}
			if ok {
				// all of it highlighted: one even band per line, under the text
				o.open("Group")
				o.open("Elements")
				for i, r := range band.rects {
					col := band.color
					if i < len(band.colors) {
						col = band.colors[i]
					}
					hs := shapeStyle{fill: col, stroke: "#00FFFFFF"}
					if err := c.shape(o, c.newID("shape"), "rect", nil, r[0], r[1], r[2], r[3], 0, hs, nil, ""); err != nil {
						return err
					}
				}
				c.text(o, b, 0, 0, wd, ht, 0, nil, 0)
				o.close("Elements")
				c.tail(o, c.newID("group"), x, y, wd, ht, fr.rot, porg, "", nil)
				o.close("Group")
				return nil
			}
			c.text(o, b, x, y, wd, ht, fr.rot, porg, 0)
			return nil
		}
		// a framed text box: the frame, and the text over it
		o.open("Group")
		o.open("Elements")
		if err := c.shape(o, c.newID("shape"), prst, adjusts(e), 0, 0, wd, ht, 0, st, nil, ""); err != nil {
			return err
		}
		c.text(o, b, 0, 0, wd, ht-20, 0, nil, st.thickness) // inside a frame 希沃 keeps the frame's height
		o.close("Elements")
		c.tail(o, c.newID("group"), x, y, wd, ht, fr.rot, porg, "", nil)
		o.close("Group")
		return nil
	case "pic":
		fr, ok := readFrame(e.FindElement("./spPr/xfrm"), t)
		if !ok {
			return nil
		}
		blip := e.FindElement(".//blip")
		if blip == nil {
			return nil
		}
		media, ok := rels[blip.SelectAttrValue("r:embed", "")]
		if !ok {
			c.warn("图片「%s」找不到文件", org.name)
			return nil
		}
		rid := c.resource(media)
		o.open("Picture")
		o.leaf("Source", "id://"+rid)
		o.leaf("Alpha", "1")
		o.open("Style")
		o.leaf("StyleType", "None")
		frameW, frameC := 0.0, ""
		if ln := e.FindElement("./spPr/ln"); ln != nil && ln.FindElement("./noFill") == nil {
			if col, ok := c.d.theme.color(ln.FindElement("./solidFill")); ok {
				frameC = col
				frameW = px(attrInt(ln, "w"))
				if frameW == 0 {
					frameW = 1 // 0.75 pt, PowerPoint's default outline, rounds to 1 px
				}
			}
		}
		shadow := e.FindElement("./spPr/effectLst/outerShdw")
		if frameC != "" || shadow != nil {
			o.leaf("PicturePresetStyle", "Empty")
		}
		if frameC != "" {
			o.open("PictureRahmenStyle")
			o.leaf("RahmenStyle", "NormalFrame")
			o.leaf("LineRahmenThickness", f(math.Round(frameW)))
			o.brush("LineRahmenBrush", frameC)
			o.close("PictureRahmenStyle")
		}
		if shadow != nil {
			col, _ := c.d.theme.color(shadow)
			o.open("Effects")
			o.open("DropShadow")
			o.leaf("Offset", f(px(attrInt(shadow, "dist"))))
			o.leaf("Angle", f(math.Mod(360-float64(attrInt(shadow, "dir"))/60000, 360)))
			o.brush("ShadowBrush", col)
			o.leaf("Opacity", "1")
			o.leaf("BlurRadius", f(px(attrInt(shadow, "blurRad"))))
			o.close("DropShadow")
			o.close("Effects")
		}
		o.close("Style")
		var clip func(*w)
		if sr := e.FindElement(".//srcRect"); sr != nil {
			clip = func(o *w) {
				o.open("PictureScaleClip")
				for _, k := range []struct{ tag, attr string }{{"LeftScale", "l"}, {"RightScale", "r"}, {"BottomScale", "b"}, {"TopScale", "t"}} {
					o.leaf(k.tag, f(float64(attrInt(sr, k.attr))/100000))
				}
				o.close("PictureScaleClip")
			}
		}
		c.tail(o, c.newID("pic"), px(int64(fr.x))-gx, px(int64(fr.y))-gy, px(int64(fr.cx)), px(int64(fr.cy)), fr.rot, porg, fr.mirror(), clip)
		o.open("PictureSourceExtension")
		o.leaf("OriginSource", "id://"+rid)
		o.close("PictureSourceExtension")
		o.close("Picture")
		return nil
	case "grpSp":
		x := e.FindElement("./grpSpPr/xfrm")
		fr, ok := readFrame(x, t)
		if !ok {
			return nil
		}
		ch, che := x.FindElement("./chOff"), x.FindElement("./chExt")
		nt := xform{1, fr.x, 1, fr.y}
		if ch != nil && che != nil && attrInt(che, "cx") > 0 && attrInt(che, "cy") > 0 {
			nt.sx = fr.cx / float64(attrInt(che, "cx"))
			nt.sy = fr.cy / float64(attrInt(che, "cy"))
			nt.tx = fr.x - float64(attrInt(ch, "x"))*nt.sx
			nt.ty = fr.y - float64(attrInt(ch, "y"))*nt.sy
		}
		if fr.rot != 0 || fr.flipH || fr.flipV {
			c.warn("组合「%s」的旋转/翻转未转换", org.name)
		}
		o.open("Group")
		o.open("Elements")
		gpx, gpy := px(int64(fr.x)), px(int64(fr.y))
		for _, k := range e.ChildElements() {
			if err := c.element(o, k, rels, nt, gpx, gpy, false); err != nil {
				return err
			}
		}
		o.close("Elements")
		c.tail(o, c.newID("group"), gpx-gx, gpy-gy, px(int64(fr.cx)), px(int64(fr.cy)), fr.rot, porg, "", nil)
		o.close("Group")
		return nil
	case "graphicFrame":
		if tbl := e.FindElement(".//graphicData/tbl"); tbl != nil {
			return c.table(o, e, tbl, t, gx, gy, porg)
		}
		c.warn("图表/对象「%s」暂不支持", org.name)
		return nil
	case "nvGrpSpPr", "grpSpPr":
		return nil
	}
	if strings.HasPrefix(e.Tag, "AlternateContent") || e.Tag == "AlternateContent" {
		c.warn("「%s」(%s) 未转换", org.name, e.Tag)
	}
	return nil
}

// text writes a Text element for a body whose box is at x, y (px) w×h.
func (c *conv) text(o *w, b body, x, y, wd, ht, rot float64, org *origin, border float64) {
	lIns, tIns, rIns := px(b.lIns), px(b.tIns), px(b.rIns)
	o.open("Text")
	c.richText(o, b, "Manual")
	o.leaf("BorderThickness", f(border))
	o.leaf("BorderType", "None")
	// 希沃 pads its text by 10 px: put the content area where PowerPoint
	// has it, as wide as PowerPoint's.
	c.tail(o, c.newID("text"), x+lIns-10, y+tIns-10, math.Max(0, wd-lIns-rIns)+20, ht+20, rot, org, "", nil)
	o.close("Text")
}

func (c *conv) resource(media string) string {
	if r, ok := c.res[media]; ok {
		return r.id
	}
	data := c.d.files[media]
	sum := md5.Sum(data)
	id := hex.EncodeToString(sum[:])
	ext := strings.ToLower(path.Ext(media))
	c.res[media] = resource{id: id, ext: ext, data: data}
	for _, k := range c.resList {
		if c.res[k].id == id {
			return id // same picture under another name
		}
	}
	c.resList = append(c.resList, media)
	sort.Strings(c.resList)
	return id
}

// motionPath turns a PowerPoint motion path, in fractions of the slide,
// into 希沃's, in px.
func (c *conv) motionPath(p string) (string, bool) {
	w, h := px(c.d.cx), px(c.d.cy)
	toks := strings.Fields(strings.ReplaceAll(p, ",", " "))
	var b strings.Builder
	isX := true
	for _, t := range toks {
		switch t {
		case "M", "L", "C", "Q":
			b.WriteString(t)
			isX = true
			continue
		case "Z", "z":
			b.WriteString("Z ")
			continue
		case "E":
			continue
		}
		v, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return "", false
		}
		if isX {
			b.WriteString(f(v*w) + ",")
		} else {
			b.WriteString(f(v*h) + " ")
		}
		isX = !isX
	}
	return b.String(), b.Len() > 0
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// paragraphBuild writes a text box whose paragraphs appear one by one: a copy
// per animated paragraph range, each showing only its paragraphs, over a base
// copy showing the rest. Every copy lays out the same text, so the visible
// parts land exactly where they would in one box.
func (c *conv) paragraphBuild(o *w, b body, x, y, wd, ht, rot float64, org origin) {
	type rng struct {
		key        string
		start, end int
	}
	var rs []rng
	in := map[int]bool{}
	for _, k := range c.paraKeys[org.id] {
		var st, en int
		fmt.Sscanf(k[strings.IndexByte(k, '#')+1:], "%d-%d", &st, &en)
		rs = append(rs, rng{k, st, en})
		for i := st; i <= en; i++ {
			in[i] = true
		}
	}
	only := func(show func(int) bool) body {
		cp := b
		cp.paras = make([]para, len(b.paras))
		for i, p := range b.paras {
			cp.paras[i] = p
			if show(i) {
				continue
			}
			cp.paras[i].runs = append([]run(nil), p.runs...)
			for j := range cp.paras[i].runs {
				cp.paras[i].runs[j].color = "#00FFFFFF"
				cp.paras[i].runs[j].highlight = ""
			}
		}
		return cp
	}
	if len(c.anims[org.id]) > 0 {
		c.warn("「%s」同时有整体动画和按段落动画，整体动画只作用于未单独设动画的段落", org.name)
	}
	base := org
	rest := false
	for i, p := range b.paras {
		for _, r := range p.runs {
			rest = rest || !in[i] && strings.TrimSpace(r.text) != ""
		}
	}
	if rest || len(c.anims[org.id]) > 0 {
		c.text(o, only(func(i int) bool { return !in[i] }), x, y, wd, ht, rot, &base, 0)
	}
	for _, r := range rs {
		cp := org
		cp.animKey = r.key
		c.text(o, only(func(i int) bool { return i >= r.start && i <= r.end }), x, y, wd, ht, rot, &cp, 0)
	}
}
