package enbxgen

import (
	"github.com/beevik/etree"
	"strings"
	"testing"
)

func convertedElement(t *testing.T, xml string, media map[string]string) string {
	t.Helper()
	d, err := readDeck(miniDeck(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range media {
		d.files[part] = []byte("media fixture " + part)
	}
	c := conv{d: d, m: fixed{}, res: map[string]resource{}, seen: map[string]bool{}}
	doc := etree.NewDocument()
	if err := doc.ReadFromString(xml); err != nil {
		t.Fatal(err)
	}
	var out w
	if err := c.element(&out, doc.Root(), media, xform{sx: 1, sy: 1}, 0, 0, true); err != nil {
		t.Fatal(err)
	}
	return out.b.String()
}

const fixtureFrame = `<xfrm><off x="0" y="0"/><ext cx="609600" cy="952500"/></xfrm>`

func TestJustifiedWordsReachRightMargin(t *testing.T) {
	xml := `<sp><nvSpPr><cNvPr id="2"/></nvSpPr><spPr>` + fixtureFrame + `<noFill/></spPr><txBody><bodyPr lIns="0" rIns="0"/><p><pPr algn="just"/><r><rPr sz="1200"/><t>ab cd third</t></r></p></txBody></sp>`
	doc := etree.NewDocument()
	if err := doc.ReadFromString(convertedElement(t, xml, nil)); err != nil {
		t.Fatal(err)
	}
	words := doc.FindElements("Group/Elements/Text")
	if len(words) != 3 {
		t.Fatalf("got %d positioned text elements", len(words))
	}
	if words[1].FindElement("RichText/Text").Text() != "cd" || words[1].FindElement("X").Text() != "38" {
		t.Fatal("second word must start at 48 px including the renderer inset")
	}
}
func TestEmbeddedAudioBecomesPlayableElement(t *testing.T) {
	xml := `<pic><nvPicPr><cNvPr id="7" name="Listening"/><nvPr><audioFile r:link="a"/></nvPr></nvPicPr><blipFill><blip r:embed="p"/></blipFill><spPr>` + fixtureFrame + `</spPr></pic>`
	got := convertedElement(t, xml, map[string]string{"a": "ppt/media/sample.mp3", "p": "ppt/media/icon.png"})
	if !strings.Contains(got, "<Audio>") || !strings.Contains(got, "<MediaName>Listening</MediaName>") {
		t.Fatal("audio became a still picture")
	}
}
func TestPlainGridStyleHasVisibleBorders(t *testing.T) {
	xml := `<graphicFrame>` + fixtureFrame + `<graphicData><tbl><tblPr><tableStyleId>{5940675A-B579-460E-94D1-54222C63F5DA}</tableStyleId></tblPr><tblGrid><gridCol w="609600"/></tblGrid><tr h="952500"><tc><tcPr><lnL><noFill/></lnL></tcPr></tc></tr></tbl></graphicData></graphicFrame>`
	got := convertedElement(t, xml, nil)
	if count := strings.Count(got, "<LineType>Normal</LineType>"); count != 3 {
		t.Fatalf("%d visible borders, want three and an explicit invisible left border", count)
	}
}
