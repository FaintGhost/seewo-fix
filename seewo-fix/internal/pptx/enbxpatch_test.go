package pptx

import (
	"strings"
	"testing"
)

func enbxRun(t string) string {
	return `<TextRun><Text>` + t + `</Text><FontSize>48</FontSize><FontFamily><Source>楷体</Source></FontFamily><Background><ColorBrush>#00FFFFFF</ColorBrush></Background><Foreground><ColorBrush>#FF000000</ColorBrush></Foreground></TextRun>`
}

// 希沃 splits runs its own way ("一位有天赋的诗人" as one run while only
// "有天赋的诗人" is highlighted): the run is cut where the highlight starts.
func TestHighlightSplitsRunAtItsEdge(t *testing.T) {
	dp := deckPara{ok: true, text: "一位有天赋的诗人。", hls: []hlRange{{len("一位"), len("一位有天赋的诗人"), "FFFF00"}}}
	line := `<TextLine><TextRuns>` + enbxRun("一位有天赋的诗人") + enbxRun("。&#xD;\n") + `</TextRuns><DefaultRunProperty>` + enbxRun("") + `</DefaultRunProperty></TextLine>`
	var st EnbxStats
	out := patchTextLine(line, dp, &st)
	if st.Highlights != 1 || st.Skipped != 0 {
		t.Fatalf("stats %+v", st)
	}
	want := `<TextRuns>` + enbxRun("一位") + strings.Replace(enbxRun("有天赋的诗人"), "#00FFFFFF", "#FFFFFF00", 1) + enbxRun("。&#xD;\n") + `</TextRuns>`
	if !strings.Contains(out, want) {
		t.Errorf("got %s", out)
	}
	if !strings.Contains(out, `<DefaultRunProperty>`+enbxRun("")) {
		t.Error("default run property touched")
	}
}

func TestHighlightSkipsParagraphThatDiffers(t *testing.T) {
	dp := deckPara{ok: true, text: "别的文字", hls: []hlRange{{0, 3, "FFFF00"}}}
	line := `<TextLine><TextRuns>` + enbxRun("一位") + `</TextRuns></TextLine>`
	var st EnbxStats
	if out := patchTextLine(line, dp, &st); out != line || st.Skipped != 1 {
		t.Errorf("changed a paragraph it could not match: %+v", st)
	}
}
