package enbxgen

import (
	"math"
	"strconv"
	"testing"

	"github.com/beevik/etree"
)

func TestRichTextLineSpacingForMappedKaiti(t *testing.T) {
	for _, tt := range []struct {
		name    string
		p       para
		mapping map[string]string
		want    float64
	}{
		{"mapped Kaiti", para{def: run{font: "楷体", sizePt: 32}, lnPct: 1}, map[string]string{"楷体": "Kaiti SC"}, 1.169491525423729},
		// Page 30: the Latin number and Chinese words share a line. The
		// renderer's fallback Chinese baseline otherwise adds height.
		{"mixed fonts", para{def: run{font: "Times New Roman", sizePt: 32}, runs: []run{{font: "Times New Roman", sizePt: 32, text: "1. "}, {font: "楷体", sizePt: 32, text: "中文"}}, lnPct: 1}, map[string]string{"楷体": "Kaiti SC"}, -0.03980891719745272},
		{"unchanged Latin", para{def: run{font: "Times New Roman", sizePt: 32}, lnPct: 1}, nil, 1.4808900212314229},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := conv{opt: Options{FontMap: tt.mapping}}
			var out w
			c.richText(&out, body{paras: []para{tt.p}}, "Manual")
			doc := etree.NewDocument()
			if err := doc.ReadFromString(out.b.String()); err != nil {
				t.Fatal(err)
			}
			node := doc.FindElement("RichText/TextLines/TextLine/LineSpacing")
			if node == nil {
				t.Fatal("missing LineSpacing")
			}
			got, err := strconv.ParseFloat(node.Text(), 64)
			if err != nil {
				t.Fatal(err)
			}
			if math.IsNaN(got) || math.Abs(got-tt.want) > 1e-6 {
				t.Fatalf("line spacing = %g, want %g", got, tt.want)
			}
		})
	}
}

func TestExplicitPointSpacing(t *testing.T) {
	p := para{def: run{font: "Times New Roman", sizePt: 32}, lnPct: 1, lnPts: 36}
	c := conv{}
	spacing := c.renderLineSpacing(p)
	// Negative leading contributes 80% on a paragraph's first line.
	fontHeight := 32.0 * 4 / 3 * 1.14990234375
	got := fontHeight + 0.8*fontHeight*(spacing-1)/10
	if math.Abs(got-48) > 1e-6 {
		t.Fatalf("36 pt spacing renders at %g px, want 48", got)
	}
}
