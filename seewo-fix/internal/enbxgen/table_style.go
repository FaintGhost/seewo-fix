package enbxgen

import (
	"cmp"
	"strings"

	"github.com/beevik/etree"
)

type cellStyle struct {
	fill    string
	text    run
	borders [4]outline // left, top, right, bottom
}

type tableFormatting struct {
	definition                                             *etree.Element
	fallbackID                                             string
	firstRow, lastRow, firstCol, lastCol, bandRow, bandCol bool
	rows, cols                                             int
}

func (c *conv) tableFormat(pr *etree.Element, name string, rows, cols int) tableFormatting {
	t := tableFormatting{rows: rows, cols: cols}
	id := c.d.defaultTableStyle
	if pr != nil {
		enabled := func(key string) bool { v := pr.SelectAttrValue(key, ""); return v == "1" || v == "true" }
		t.firstRow, t.lastRow = enabled("firstRow"), enabled("lastRow")
		t.firstCol, t.lastCol = enabled("firstCol"), enabled("lastCol")
		t.bandRow, t.bandCol = enabled("bandRow"), enabled("bandCol")
		t.definition = pr.FindElement("./tableStyle")
		if ref := pr.FindElement("./tableStyleId"); ref != nil {
			id = strings.TrimSpace(ref.Text())
		}
	}
	id = strings.ToUpper(id)
	if t.definition == nil {
		t.definition = c.d.tableStyles[id]
	}
	if t.definition == nil {
		t.fallbackID = id
		switch id {
		case "", mediumStyle2, "{5940675A-B579-460E-94D1-54222C63F5DA}", "{2D5ABB26-0587-4C30-8999-92F81FD0307C}":
		default:
			c.warn("表格「%s」的样式 %s 没有内嵌定义，按无样式处理", name, id)
		}
	}
	return t
}

func (c *conv) cellFormatting(t tableFormatting, row, col, rowSpan, colSpan int) cellStyle {
	s := cellStyle{fill: "#00FFFFFF"}
	for i := range s.borders {
		s.borders[i].color = "#00FFFFFF"
	}
	if t.definition == nil {
		// Office can omit definitions for built-in styles. Resolve the common
		// defaults only when the deck supplies no definition of its own.
		switch t.fallbackID {
		case mediumStyle2:
			for i := range s.borders {
				s.borders[i] = outline{px(12700), "#FFFFFFFF"}
			}
			accent := cmp.Or(c.d.theme.colors["accent1"], "000000")
			if t.firstRow && row == 0 {
				s.fill = "#FF" + accent
				s.text = run{bold: true, color: "#FF" + cmp.Or(c.d.theme.colors[c.d.theme.clrMap["bg1"]], "FFFFFF")}
			} else {
				mod := etree.NewElement("schemeClr")
				tint := "20000"
				if t.bandRow && (row-btoi(t.firstRow))%2 == 0 {
					tint = "40000"
				}
				mod.CreateElement("tint").CreateAttr("val", tint)
				s.fill = applyMods(accent, mod)
			}
		case "{5940675A-B579-460E-94D1-54222C63F5DA}":
			for i := range s.borders {
				s.borders[i] = outline{px(12700), "#FF" + cmp.Or(c.d.theme.colors[c.d.theme.clrMap["tx1"]], "000000")}
			}
		}
		return s
	}
	// A border on a region applies at its perimeter. Within the same
	// region, the insideH/insideV declarations govern the shared edges.
	apply := func(name string, top, left, bottom, right int) {
		region := t.definition.SelectElement(name)
		if region == nil {
			return
		}
		if text := region.SelectElement("tcTxStyle"); text != nil {
			if color, ok := c.d.theme.color(text); ok {
				s.text.color = color
			}
			for attr, target := range map[string]*bool{"b": &s.text.bold, "i": &s.text.italic} {
				switch text.SelectAttrValue(attr, "") {
				case "on", "1", "true":
					*target = true
				case "off", "0", "false":
					*target = false
				}
			}
		}
		style := region.SelectElement("tcStyle")
		if style == nil {
			return
		}
		if fill := style.SelectElement("fill"); fill != nil {
			s.fill = c.cellFill(fill, s.fill)
		}
		borders := style.SelectElement("tcBdr")
		if borders == nil {
			return
		}
		names := [4]string{"insideV", "insideH", "insideV", "insideH"}
		if col == left {
			names[0] = "left"
		}
		if row == top {
			names[1] = "top"
		}
		if col+colSpan >= right {
			names[2] = "right"
		}
		if row+rowSpan >= bottom {
			names[3] = "bottom"
		}
		for i, key := range names {
			if side := borders.SelectElement(key); side != nil {
				s.borders[i] = c.cellOutline(side.SelectElement("ln"), s.borders[i])
			}
		}
	}
	apply("wholeTbl", 0, 0, t.rows, t.cols)
	bands := [2]string{"band1H", "band2H"}
	if t.bandRow && row >= btoi(t.firstRow) {
		apply(bands[(row-btoi(t.firstRow))%2], row, 0, row+1, t.cols)
	}
	bands = [2]string{"band1V", "band2V"}
	if t.bandCol && col >= btoi(t.firstCol) {
		apply(bands[(col-btoi(t.firstCol))%2], 0, col, t.rows, col+1)
	}
	firstC, lastC := col == 0 && t.firstCol, col+colSpan >= t.cols && t.lastCol
	firstR, lastR := row == 0 && t.firstRow, row+rowSpan >= t.rows && t.lastRow
	if lastC {
		apply("lastCol", 0, col, t.rows, t.cols)
	}
	if firstC {
		apply("firstCol", 0, 0, t.rows, 1)
	}
	if lastR {
		apply("lastRow", row, 0, t.rows, t.cols)
	}
	if firstR {
		apply("firstRow", 0, 0, 1, t.cols)
	}
	var corner string
	switch {
	case firstR && firstC:
		corner = "nwCell"
	case firstR && lastC:
		corner = "neCell"
	case lastR && firstC:
		corner = "swCell"
	case lastR && lastC:
		corner = "seCell"
	}
	if corner != "" {
		apply(corner, row, col, row+rowSpan, col+colSpan)
	}
	return s
}
func (c *conv) cellFill(pr *etree.Element, inherited string) string {
	if pr != nil {
		if pr.SelectElement("noFill") != nil {
			return "#00FFFFFF"
		}
		if color, ok := c.d.theme.color(pr.SelectElement("solidFill")); ok {
			return color
		}
	}
	return inherited
}
func (c *conv) cellOutline(line *etree.Element, inherited outline) outline {
	if line == nil {
		return inherited
	}
	if line.SelectElement("noFill") != nil {
		return outline{0, "#00FFFFFF"}
	}
	if color, ok := c.d.theme.color(line.SelectElement("solidFill")); ok {
		inherited.color = color
		if inherited.thickness == 0 {
			inherited.thickness = px(12700)
		}
	}
	if line.SelectAttr("w") != nil {
		inherited.thickness = px(attrInt(line, "w"))
	}
	return inherited
}
