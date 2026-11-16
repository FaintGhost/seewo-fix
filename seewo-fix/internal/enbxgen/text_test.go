package enbxgen

import (
	"testing"

	"github.com/beevik/etree"
)

func TestLineBreakingUsesParagraphIndentInEMU(t *testing.T) {
	d, err := readDeck(miniDeck(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, properties string
		firstLineLength  int
	}{
		{"no indent", `<pPr/>`, 14},
		{"left margin", `<pPr marL="457200"/>`, 8},
		{"first line indent", `<pPr indent="457200"/>`, 8},
		{"hanging bullet", `<pPr marL="457200" indent="-457200"><buChar char="•"/></pPr>`, 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc := etree.NewDocument()
			if err := doc.ReadFromString(`<txBody><bodyPr lIns="0" rIns="0"/><p>` + tt.properties + `<r><rPr sz="1800"><latin typeface="Times New Roman"/></rPr><t>one two three four five</t></r></p></txBody>`); err != nil {
				t.Fatal(err)
			}
			c := conv{d: d, m: fixed{}}
			b := c.readBody(doc.Root(), int64(180*emuPerPx), "")
			lines := b.paras[0].lines
			if len(lines) < 2 || lines[0].length != tt.firstLineLength {
				t.Fatalf("wrapped lines = %+v, first line should contain %d UTF-16 units", lines, tt.firstLineLength)
			}
		})
	}
}
