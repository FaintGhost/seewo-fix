package enbxgen

import (
	"strconv"

	"github.com/beevik/etree"
)

// mediumStyle2 is PowerPoint's default table style, "Medium Style 2 –
// Accent 1", the one WPS writes too.
const mediumStyle2 = "{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"

type outline struct {
	thickness float64
	color     string
}

// table writes an a:tbl as 希沃's Table.
func (c *conv) table(o *w, e, tbl *etree.Element, t xform, gx, gy float64, org *origin) error {
	fr, ok := readFrame(e.FindElement("./xfrm"), t)
	if !ok {
		return nil
	}
	pr := tbl.FindElement("./tblPr")
	var cols []int64
	for _, g := range tbl.FindElements("./tblGrid/gridCol") {
		cols = append(cols, attrInt(g, "w"))
	}
	o.open("Table")
	o.open("Skin")
	o.leaf("TableSkin", "Gray")
	o.brush("HeaderFill", "#FFFFFFFF")
	o.open("RowSkins")
	for i := 0; i < 2; i++ {
		o.open("RowSkinDetail")
		o.brush("Fill", "#FFFFFFFF")
		o.close("RowSkinDetail")
	}
	o.close("RowSkins")
	o.leaf("StrokeThickness", f(px(12700)))
	o.brush("Stroke", "#FFFFFFFF")
	o.brush("HeaderForeground", "#FF000000")
	o.leaf("HeaderFontSize", "20")
	o.close("Skin")
	o.open("ColumnWidths")
	for _, w := range cols {
		o.leaf("Item", f(px(w)))
	}
	o.close("ColumnWidths")
	o.open("Rows")
	rows := tbl.FindElements("./tr")
	format := c.tableFormat(pr, org.name, len(rows), len(cols))
	heights := c.tableRowHeights(tbl, cols, format)
	for ri, tr := range rows {
		o.open("Row")
		o.leaf("Height", f(heights[ri]))
		o.open("Cells")
		tcs := tr.FindElements("./tc")
		regionEnd := 0 // grid column after the merge region covering ci
		for ci, tc := range tcs {
			span := int(attrInt(tc, "gridSpan"))
			if span < 1 {
				span = 1
			}
			hidden := tc.SelectAttrValue("hMerge", "0") == "1"
			if !hidden {
				regionEnd = ci + span
			} else if regionEnd > ci {
				// 希沃 gives a covered cell the rest of its region
				span = regionEnd - ci
			}
			last := tcs[min(len(tcs)-1, max(ci, regionEnd-1))]
			rspan := int(attrInt(tc, "rowSpan"))
			if rspan < 1 {
				rspan = 1
			}
			cellW := int64(0)
			for k := ci; k < ci+span && k < len(cols); k++ {
				cellW += cols[k]
			}
			tcPr := tc.FindElement("./tcPr")
			ins := tableCellInsets(tcPr)
			style := c.cellFormatting(format, ri, ci, rspan, span)
			fill := c.cellFill(tcPr, style.fill)
			o.open("Cell")
			o.open("Text")
			c.cellInsets = &ins
			tb := tc.FindElement("./txBody")
			var b body
			if tb != nil {
				b = c.readStyledBody(tb, cellW, style.text)
			} else {
				b = c.readStyledBody(etree.NewElement("txBody"), cellW, style.text)
				b.paras = []para{{align: "Left", lnPct: 1, lines: []lineSpan{{0, 0}}, def: run{sizePt: float64(c.d.defSz) / 100, font: c.d.theme.minor["latin"], color: "#FF000000"}}}
			}
			c.cellInsets = nil
			if tcPr != nil {
				switch tcPr.SelectAttrValue("anchor", "t") {
				case "ctr":
					b.anchor = "ctr"
				case "b":
					b.anchor = "b"
				}
			}
			c.richText(o, b, "Height")
			o.leaf("BorderThickness", "0")
			o.leaf("BorderType", "None")
			for _, k := range []string{"X", "Y", "Width", "Height"} {
				o.leaf(k, "0")
			}
			o.leaf("Rotation", "0")
			o.leaf("IsLocked", "False")
			o.leaf("CanClone", "False")
			o.leaf("HasMask", "False")
			o.close("Text")
			o.leaf("HMerged", boolStr(tc.SelectAttrValue("hMerge", "0") == "1"))
			o.leaf("VMerged", boolStr(tc.SelectAttrValue("vMerge", "0") == "1"))
			o.leaf("RowSpan", strconv.Itoa(rspan))
			o.leaf("ColumnSpan", strconv.Itoa(span))
			o.brush("Background", fill)
			o.leaf("IsErasable", "False")
			o.open("TableCellTextMarginExtension")
			o.leaf("TextMargin", f(px(ins[0]))+","+f(px(ins[1]))+","+f(px(ins[2]))+","+f(px(ins[3])))
			o.close("TableCellTextMarginExtension")
			o.open("TableCellBoundsOutlineExtension")
			for i, side := range []struct{ tag, ln string }{{"LeftOutline", "lnL"}, {"TopOutline", "lnT"}, {"RightOutline", "lnR"}, {"BottomOutline", "lnB"}} {
				ol := style.borders[i]
				src := tcPr
				if hidden && side.ln == "lnR" && last != tc {
					src = last.FindElement("./tcPr")
				}
				if src != nil {
					ol = c.cellOutline(src.SelectElement(side.ln), ol)
				}
				o.open(side.tag)
				o.leaf("Thickness", f(ol.thickness))
				o.brush("Foreground", ol.color)
				if ol.thickness > 0 {
					o.leaf("LineType", "Normal")
				} else {
					o.leaf("LineType", "None")
				}
				o.close(side.tag)
			}
			o.close("TableCellBoundsOutlineExtension")
			o.close("Cell")
		}
		o.close("Cells")
		o.close("Row")
	}
	o.close("Rows")
	o.leaf("CellHPadding", "24")
	o.leaf("CellVPadding", "24")
	var height float64
	for _, h := range heights {
		height += h
	}
	c.tail(o, c.newID("table"), px(int64(fr.x))-gx, px(int64(fr.y))-gy, px(int64(fr.cx)), height, fr.rot, org, "", nil)
	o.close("Table")
	return nil
}

func tableCellInsets(pr *etree.Element) [4]int64 {
	ins := [4]int64{91440, 45720, 91440, 45720}
	if pr != nil {
		for i, key := range []string{"marL", "marT", "marR", "marB"} {
			if pr.SelectAttr(key) != nil {
				ins[i] = attrInt(pr, key)
			}
		}
	}
	return ins
}

// WPS can save minimum row heights smaller than the rendered text. PowerPoint
// expands them using its 1.2-em line box and cell margins; 希沃 uses the stored
// row heights for its borders. Resolve the minimums before emitting the table.
func (c *conv) tableRowHeights(tbl *etree.Element, cols []int64, format tableFormatting) []float64 {
	rows := tbl.FindElements("./tr")
	heights := make([]float64, len(rows))
	for i, row := range rows {
		heights[i] = px(attrInt(row, "h"))
	}
	type mergedHeight struct {
		start, end int
		height     float64
	}
	var merged []mergedHeight
	previous := c.cellInsets
	defer func() { c.cellInsets = previous }()
	for ri, row := range rows {
		for ci, cell := range row.FindElements("./tc") {
			if cell.SelectAttrValue("hMerge", "0") == "1" || cell.SelectAttrValue("vMerge", "0") == "1" {
				continue
			}
			span := max(1, int(attrInt(cell, "gridSpan")))
			var width int64
			for col := ci; col < min(ci+span, len(cols)); col++ {
				width += cols[col]
			}
			ins := tableCellInsets(cell.FindElement("./tcPr"))
			c.cellInsets = &ins
			tb := cell.FindElement("./txBody")
			if tb == nil {
				continue
			}
			style := c.cellFormatting(format, ri, ci, max(1, int(attrInt(cell, "rowSpan"))), span)
			b := c.readStyledBody(tb, width, style.text)
			h := px(ins[1] + ins[3])
			for _, p := range b.paras {
				h += float64(len(p.lines))*p.lineHeight() + p.spcBef + p.spcAft
			}
			end := min(ri+max(1, int(attrInt(cell, "rowSpan"))), len(rows))
			if end == ri+1 {
				heights[ri] = max(heights[ri], h)
			} else {
				merged = append(merged, mergedHeight{ri, end, h})
			}
		}
	}
	for _, cell := range merged {
		var available float64
		for _, h := range heights[cell.start:cell.end] {
			available += h
		}
		heights[cell.end-1] += max(0, cell.height-available)
	}
	return heights
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
