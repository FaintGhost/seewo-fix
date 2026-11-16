package pptx

import (
	"regexp"
	"strconv"
)

// Nearly straight lines.
//
// A line drawn by hand is rarely exactly level: 直接连接符 45 on page 3 is
// 375.7 px long and 1.5 px high. PowerPoint draws it as the underline it is;
// 希沃 draws such a line visibly shorter at both ends. Lines that are level to
// the eye are made exactly level (or exactly vertical), keeping their middle.

// StraightenPx is the largest drift, in px, that a line may have and still be
// taken as meant to be level; StraightenSlope is the largest slope.
const (
	StraightenPx    = 4
	StraightenSlope = 0.02
)

var (
	reLineGeom = regexp.MustCompile(`prstGeom prst="(?:line|straightConnector1)"`)
	reFirstOff = regexp.MustCompile(`<a:off x="(-?\d+)" y="(-?\d+)"\s*/>\s*<a:ext cx="(\d+)" cy="(\d+)"\s*/>`)
	reRot      = regexp.MustCompile(`<a:xfrm\b[^>]*\brot="`)
)

// FixLines straightens nearly level or nearly vertical lines of a part and
// returns the ids it changed.
func FixLines(data []byte) ([]byte, []string, error) {
	doc, err := Scan(data)
	if err != nil {
		return nil, nil, err
	}
	var edits []Edit
	var ids []string
	var walk func(s *Shape)
	walk = func(s *Shape) {
		for _, ch := range s.Children {
			walk(ch)
		}
		if s.Kind != "p:cxnSp" && s.Kind != "p:sp" {
			return
		}
		x := data[s.TagStart:s.EndEnd]
		if !reLineGeom.Match(x) || reRot.Match(x) {
			return
		}
		m := reFirstOff.FindSubmatchIndex(x)
		if m == nil {
			return
		}
		val := func(i int) int64 {
			v, _ := strconv.ParseInt(string(x[m[2*i]:m[2*i+1]]), 10, 64)
			return v
		}
		ox, oy, cx, cy := val(1), val(2), val(3), val(4)
		set := func(i int, v int64) {
			edits = append(edits, Edit{s.TagStart + m[2*i], s.TagStart + m[2*i+1], strconv.FormatInt(v, 10)})
		}
		limit := int64(StraightenPx * EMUPerPx)
		switch {
		case cy > 0 && cy <= limit && float64(cy) <= StraightenSlope*float64(cx):
			set(2, oy+cy/2)
			set(4, 0)
		case cx > 0 && cx <= limit && float64(cx) <= StraightenSlope*float64(cy):
			set(1, ox+cx/2)
			set(3, 0)
		default:
			return
		}
		ids = append(ids, s.ID)
	}
	for _, s := range doc.Roots {
		walk(s)
	}
	if len(edits) == 0 {
		return data, nil, nil
	}
	SortEdits(edits)
	return Apply(data, edits), ids, nil
}
