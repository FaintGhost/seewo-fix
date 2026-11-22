package enbxgen

import (
	"fmt"
	"github.com/beevik/etree"
	"path"
	"strings"
	"testing"
)

func TestPictureShadowUsesCanvasDirection(t *testing.T) {
	for _, deg := range []int{0, 45, 90, 180, 270, 315} {
		xml := fmt.Sprintf(`<pic><blipFill><blip r:embed="p"/></blipFill><spPr>%s<effectLst><outerShdw dir="%d" dist="19050"><srgbClr val="102030"/></outerShdw></effectLst></spPr></pic>`, fixtureFrame, deg*60000)
		got := convertedElement(t, xml, map[string]string{"p": "ppt/media/icon.png"})
		if !strings.Contains(got, fmt.Sprintf("<Angle>%d</Angle>", (deg+90)%360)) {
			t.Errorf("incorrect shadow direction for %d°", deg)
		}
	}
}

func TestShadowStaysBehindEditableRectangle(t *testing.T) {
	for _, text := range []string{"", `<txBody><bodyPr/><p><r><t>Editable</t></r></p></txBody>`} {
		xml := `<sp><nvSpPr><cNvPr id="42" name="Card"/></nvSpPr><spPr>` + fixtureFrame + `<solidFill><srgbClr val="EEFFFF"/></solidFill><effectLst><outerShdw dir="13500000" dist="76200" blurRad="38100"><srgbClr val="235270"><alpha val="44000"/></srgbClr></outerShdw></effectLst></spPr>` + text + `</sp>`
		doc := etree.NewDocument()
		if err := doc.ReadFromString(convertedElement(t, xml, nil)); err != nil {
			t.Fatal(err)
		}
		group := doc.Root()
		elements := group.FindElements("Elements/*")
		if len(elements) < 2 || elements[0].Tag != "Picture" || elements[1].Tag != "Shape" || group.FindElement("ElementPptxOriginId/OriginPptxId").Text() != "42" {
			t.Fatal("shadow and editable geometry must share the original animation target")
		}
	}
}

func TestSlideThemeFollowsMasterAndLocalMap(t *testing.T) {
	d := &deck{files: map[string][]byte{}}
	link := func(part, kind, target string) {
		dir, name := path.Split(part)
		d.files[dir+"_rels/"+name+".rels"] = []byte(fmt.Sprintf(`<Relationships><Relationship Id="r" Type="types/%s" Target="%s"/></Relationships>`, kind, target))
	}
	for _, color := range []string{"148370", "BA691F"} {
		slide := "ppt/slides/" + color + ".xml"
		layout := "ppt/layouts/" + color + ".xml"
		master := "ppt/masters/" + color + ".xml"
		theme := "ppt/design/" + color + ".xml"
		link(slide, "slideLayout", "/"+layout)
		link(layout, "slideMaster", "/"+master)
		link(master, "theme", "/"+theme)
		d.files[layout] = []byte(`<layout><clrMapOvr><overrideClrMapping tx1="accent1"/></clrMapOvr></layout>`)
		d.files[master] = []byte(`<master><clrMap tx1="dk1"/></master>`)
		d.files[theme] = []byte(`<theme><clrScheme><dk1><srgbClr val="010203"/></dk1><accent1><srgbClr val="` + color + `"/></accent1></clrScheme></theme>`)
		root := etree.NewElement("slide")
		fill := etree.NewElement("solidFill")
		fill.CreateElement("schemeClr").CreateAttr("val", "tx1")
		local := d.slideTheme(slide, root)
		if got, _ := local.color(fill); got != "#FF"+color {
			t.Fatalf("layout points to wrong theme: %s", got)
		}
		root.CreateElement("clrMapOvr").CreateElement("masterClrMapping")
		local = d.slideTheme(slide, root)
		if got, _ := local.color(fill); got != "#FF010203" {
			t.Fatalf("master mapping mutated through a sibling slide: %s", got)
		}
	}
}

func TestInlineTableStyleAndRunInheritance(t *testing.T) {
	// Inline definitions use the same cascade as style IDs in the package part.
	xml := `<graphicFrame>` + fixtureFrame + `<graphicData><tbl><tblPr firstRow="true" bandRow="1"><tableStyle styleId="new"><wholeTbl><tcStyle><fill><solidFill><schemeClr val="accent1"/></solidFill></fill></tcStyle></wholeTbl><firstRow><tcTxStyle b="on"><srgbClr val="FDFDFC"/></tcTxStyle></firstRow><band1H><tcStyle><fill><solidFill><srgbClr val="BBBBBC"/></solidFill></fill></tcStyle></band1H></tableStyle></tblPr><tblGrid><gridCol w="609600"/></tblGrid><tr h="952500"><tc><txBody><bodyPr/><p><pPr><defRPr sz="1650"/></pPr><r><t>A</t></r><r><rPr b="0"/><t>B</t></r></p></txBody></tc></tr><tr h="952500"><tc/></tr><tr h="952500"><tc><tcPr><solidFill><srgbClr val="CC00CC"/></solidFill></tcPr></tc></tr></tbl></graphicData></graphicFrame>`
	doc := etree.NewDocument()
	if err := doc.ReadFromString(convertedElement(t, xml, nil)); err != nil {
		t.Fatal(err)
	}
	cells := doc.FindElements("Table/Rows/Row/Cells/Cell")
	for i, col := range []string{"#FF4874CB", "#FFBBBBBC", "#FFCC00CC"} {
		if got := cells[i].FindElement("Background/ColorBrush").Text(); got != col {
			t.Errorf("cell %d = %s, want %s", i, got, col)
		}
	}
	runs := cells[0].FindElements("Text/RichText/TextLines/TextLine/TextRuns/TextRun")
	for i, weight := range []string{"Bold", "Normal"} {
		if runs[i].FindElement("FontWeight").Text() != weight || runs[i].FindElement("FontSize").Text() != "22" || runs[i].FindElement("Foreground/ColorBrush").Text() != "#FFFDFDFC" {
			t.Errorf("run %d lost inherited or local formatting", i)
		}
	}
}
