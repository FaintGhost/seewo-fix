package enbxgen

import (
	"math"
	"strconv"
	"testing"

	"github.com/beevik/etree"
)

func TestTableRowsExpandForText(t *testing.T) {
	// WPS stores 30 pt rows even though a 32 pt line plus the default
	// 3.6 pt top/bottom margins needs 45.6 pt in PowerPoint.
	doc := etree.NewDocument()
	if err := doc.ReadFromString(`<tbl><tr h="381000"><tc><txBody><bodyPr/><p><r><rPr sz="3200"><latin typeface="Times New Roman"/></rPr><t>that</t></r></p></txBody><tcPr/></tc></tr><tr h="1143000"><tc><txBody><bodyPr/><p><r><rPr sz="3200"><latin typeface="Times New Roman"/></rPr><t>who</t></r></p></txBody><tcPr/></tc></tr></tbl>`); err != nil {
		t.Fatal(err)
	}
	d, err := readDeck(miniDeck(t))
	if err != nil {
		t.Fatal(err)
	}
	c := conv{d: d, m: fixed{}}
	heights := emittedRowHeights(t, &c, doc.Root(), 4762500)
	for i, want := range []float64{60.8, 120} {
		if math.Abs(heights[i]-want) > 1e-6 {
			t.Errorf("row %d height = %g, want %g", i, heights[i], want)
		}
	}
}

func TestTableRowsUseWrappedTextAndCellMargins(t *testing.T) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(`<tbl><tr h="579120"><tc><txBody><bodyPr/><p><r><rPr sz="3200"><latin typeface="Times New Roman"/></rPr><t>one two</t></r></p></txBody><tcPr marT="137160" marB="137160"/></tc></tr></tbl>`); err != nil {
		t.Fatal(err)
	}
	d, err := readDeck(miniDeck(t))
	if err != nil {
		t.Fatal(err)
	}
	c := conv{d: d, m: fixed{}}
	// 100 px content width wraps this fixed-width fixture into two lines.
	heights := emittedRowHeights(t, &c, doc.Root(), 1135380)
	if math.Abs(heights[0]-131.2) > 1e-6 {
		t.Fatalf("two-line row = %g px, want 98.4 pt (131.2 px)", heights[0])
	}
}

func emittedRowHeights(t *testing.T, c *conv, tbl *etree.Element, width int64) []float64 {
	t.Helper()
	frame := etree.NewElement("graphicFrame")
	x := frame.CreateElement("xfrm")
	off := x.CreateElement("off")
	off.CreateAttr("x", "0")
	off.CreateAttr("y", "0")
	ext := x.CreateElement("ext")
	ext.CreateAttr("cx", strconv.FormatInt(width, 10))
	ext.CreateAttr("cy", "1524000")
	tbl.CreateElement("tblGrid").CreateElement("gridCol").CreateAttr("w", strconv.FormatInt(width, 10))
	var out w
	if err := c.table(&out, frame, tbl, xform{sx: 1, sy: 1}, 0, 0, &origin{id: "2"}); err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromString(out.b.String()); err != nil {
		t.Fatal(err)
	}
	var heights []float64
	for _, row := range doc.FindElements("Table/Rows/Row") {
		h, err := strconv.ParseFloat(row.FindElement("Height").Text(), 64)
		if err != nil {
			t.Fatal(err)
		}
		heights = append(heights, h)
	}
	if len(heights) != len(tbl.FindElements("tr")) {
		t.Fatalf("lost table rows: %v", heights)
	}
	return heights
}

func TestLatinPunctuationCanHangAtLineEnd(t *testing.T) {
	c := conv{m: fixed{}}
	// The letters fit; PowerPoint lets the final punctuation cross the margin.
	for _, text := range []string{"word.", "word,"} {
		lines := c.breakLines([]run{{text: text, font: "Times New Roman", sizePt: 32}}, 32*12700*2, true, 0)
		if len(lines) != 1 || lines[0].length != 5 {
			t.Errorf("%q caused a spurious wrap: %+v", text, lines)
		}
	}
}
