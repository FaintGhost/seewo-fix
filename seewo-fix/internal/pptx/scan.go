// Package pptx edits PresentationML parts by byte-level surgery.
//
// It never re-serialises XML: every change is a replacement of an attribute
// value or of one element's start/end tag, applied to the original bytes.
// That keeps namespace prefixes, attribute order, comments, whitespace and
// every untouched part bit-identical, which matters because a PowerPoint /
// WPS reader is stricter about a re-serialised document than about the
// original one.
package pptx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// parentIsText reports whether the innermost open element is an a:t.
func parentIsText(elems []string) bool {
	return len(elems) > 0 && elems[len(elems)-1] == "a:t"
}

// geomPrefix namespaces the attributes of a:off / a:ext / a:chOff / a:chExt,
// which all use x/y/cx/cy, in Xfrm.Attrs.
var geomPrefix = map[string]string{
	"a:off":   "off.",
	"a:ext":   "ext.",
	"a:chOff": "chOff.",
	"a:chExt": "chExt.",
}

// Shape kinds that carry their own <a:xfrm> geometry.
var shapeKinds = map[string]bool{
	"p:sp": true, "p:pic": true, "p:grpSp": true,
	"p:graphicFrame": true, "p:cxnSp": true,
}

// Attr is one attribute of an element, with the byte span of its value.
type Attr struct {
	Name     string
	ValStart int
	ValEnd   int
}

// Xfrm is one a:xfrm / p:xfrm element of a shape.
type Xfrm struct {
	IsGroup bool // belongs to p:grpSpPr, i.e. it is the group's own transform
	Attrs   map[string]Attr
	rot     bool // rot / flipH / flipV present with a non-zero value
}

// Shape is one drawable or group in a shape tree.
type Shape struct {
	Kind string

	TagStart, TagEnd int // byte span of its start tag
	BodyStart        int // just after its start tag
	EndStart         int // byte offset of "</kind"
	EndEnd           int // just after "</kind>"

	Xfrm     *Xfrm
	Parent   *Shape
	Children []*Shape

	// IsTextBox is a shape the author made as a text box (cNvSpPr txBox="1");
	// HasText is a shape carrying any non-blank text run; Insets are the text
	// body's l/t/r/b insets in EMU, PowerPoint's defaults when implicit.
	IsTextBox bool
	HasText   bool
	Insets    [4]int64

	// PrefixEnd is the byte offset just after the group's <p:grpSpPr>…</p:grpSpPr>,
	// i.e. where an ungrouped shape's children start.
	PrefixEnd int
	ID        string
}

// Doc is the scanned form of one XML part.
type Doc struct {
	Data  []byte
	Roots []*Shape
	// BodyPrs are the byte spans of a:bodyPr start tags, for inset patching.
	BodyPrs [][2]int
	// AutoFits are the byte spans of a:spAutoFit start tags.
	AutoFits [][2]int
}

func fullName(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// Scan walks one part and collects its shape tree.
func Scan(data []byte) (*Doc, error) {
	doc := &Doc{Data: data}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true

	var elems []string
	var shapes []*Shape
	var open []*Xfrm // one entry per open xfrm element, nil when it belongs to no shape

	for {
		before := dec.InputOffset()
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse xml at byte %d: %w", before, err)
		}
		after := dec.InputOffset()

		switch t := tok.(type) {
		case xml.StartElement:
			name := fullName(t.Name)
			parent := ""
			if n := len(elems); n > 0 {
				parent = elems[n-1]
			}

			if shapeKinds[name] {
				s := &Shape{Kind: name, TagStart: int(before), TagEnd: int(after), BodyStart: int(after),
					Insets: [4]int64{defaultLIns, defaultTIns, defaultRIns, defaultBIns}}
				for _, a := range t.Attr {
					if a.Name.Local == "id" {
						s.ID = a.Value
					}
				}
				if n := len(shapes); n > 0 {
					s.Parent = shapes[n-1]
					s.Parent.Children = append(s.Parent.Children, s)
				} else {
					doc.Roots = append(doc.Roots, s)
				}
				shapes = append(shapes, s)
			} else if name == "a:xfrm" || name == "p:xfrm" {
				x := &Xfrm{Attrs: map[string]Attr{}, IsGroup: parent == "p:grpSpPr"}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "rot", "flipH", "flipV":
						if a.Value != "" && a.Value != "0" {
							x.rot = true
						}
					}
				}
				// Attach to the innermost shape that has no transform yet: schema
				// order puts a shape's own xfrm before its children.
				if n := len(shapes); n > 0 && shapes[n-1].Xfrm == nil {
					shapes[n-1].Xfrm = x
					open = append(open, x)
				} else {
					open = append(open, nil)
				}
			} else if keyPrefix, isGeom := geomPrefix[name]; isGeom {
				if n := len(open); n > 0 && open[n-1] != nil && (parent == "a:xfrm" || parent == "p:xfrm") {
					x := open[n-1]
					spans := attrSpans(data, int(before), int(after))
					for _, a := range t.Attr {
						if s, ok := spans[a.Name.Local]; ok {
							x.Attrs[keyPrefix+a.Name.Local] = s
						}
					}
				}
			} else if name == "p:cNvPr" {
				if n := len(shapes); n > 0 && shapes[n-1].ID == "" {
					for _, a := range t.Attr {
						if a.Name.Local == "id" {
							shapes[n-1].ID = a.Value
						}
					}
				}
			} else if name == "p:cNvSpPr" {
				if n := len(shapes); n > 0 {
					for _, a := range t.Attr {
						if a.Name.Local == "txBox" && a.Value == "1" {
							shapes[n-1].IsTextBox = true
						}
					}
				}
			} else if name == "a:bodyPr" {
				if n := len(shapes); n > 0 {
					sc := shapes[n-1]
					for _, a := range t.Attr {
						var idx int
						switch a.Name.Local {
						case "lIns":
							idx = 0
						case "tIns":
							idx = 1
						case "rIns":
							idx = 2
						case "bIns":
							idx = 3
						default:
							continue
						}
						if v, err := strconv.ParseInt(a.Value, 10, 64); err == nil {
							sc.Insets[idx] = v
						}
					}
				}
				doc.BodyPrs = append(doc.BodyPrs, [2]int{int(before), int(after)})
			} else if name == "a:spAutoFit" {
				doc.AutoFits = append(doc.AutoFits, [2]int{int(before), int(after)})
			}
			elems = append(elems, name)

		case xml.CharData:
			if n := len(shapes); n > 0 && parentIsText(elems) && strings.TrimSpace(string(t)) != "" {
				shapes[n-1].HasText = true
			}

		case xml.EndElement:
			name := fullName(t.Name)
			if shapeKinds[name] {
				if n := len(shapes); n > 0 {
					s := shapes[n-1]
					s.EndStart, s.EndEnd = int(before), int(after)
					shapes = shapes[:n-1]
				}
			} else if name == "a:xfrm" || name == "p:xfrm" {
				if n := len(open); n > 0 {
					open = open[:n-1]
				}
			} else if name == "p:grpSpPr" {
				if n := len(shapes); n > 0 {
					shapes[n-1].PrefixEnd = int(after)
				}
			}
			if n := len(elems); n > 0 {
				elems = elems[:n-1]
			}
		}
	}
	return doc, nil
}

// attrSpans finds name="value" pairs inside one start tag.
func attrSpans(data []byte, start, end int) map[string]Attr {
	out := map[string]Attr{}
	tag := data[start:end]
	i := 0
	for i < len(tag) {
		// find next '=' then walk back over the name, forward over the quote
		eq := bytes.IndexByte(tag[i:], '=')
		if eq < 0 {
			break
		}
		eq += i
		j := eq - 1
		for j >= 0 && (isNameByte(tag[j])) {
			j--
		}
		name := string(tag[j+1 : eq])
		k := eq + 1
		for k < len(tag) && (tag[k] == ' ' || tag[k] == '\n' || tag[k] == '\r' || tag[k] == '\t') {
			k++
		}
		if k < len(tag) && (tag[k] == '"' || tag[k] == '\'') {
			quote := tag[k]
			vstart := k + 1
			vend := vstart
			for vend < len(tag) && tag[vend] != quote {
				vend++
			}
			out[name] = Attr{Name: name, ValStart: start + vstart, ValEnd: start + vend}
			i = vend + 1
		} else {
			i = k
		}
	}
	return out
}

func isNameByte(b byte) bool {
	return b == ':' || b == '-' || b == '_' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// Edit replaces data[start:end] with Text.
type Edit struct {
	Start, End int
	Text       string
}

// Apply writes the edits into a copy of data. Edits must not overlap; they are
// applied back to front so earlier spans stay valid.
func Apply(data []byte, edits []Edit) []byte {
	if len(edits) == 0 {
		return data
	}
	out := make([]byte, len(data))
	copy(out, data)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		if e.Start < 0 || e.End > len(out) || e.Start > e.End {
			panic(fmt.Sprintf("bad edit %d:%d", e.Start, e.End))
		}
		out = append(out[:e.Start], append([]byte(e.Text), out[e.End:]...)...)
	}
	return out
}

// SortEdits orders edits by position so Apply can go back to front.
func SortEdits(edits []Edit) {
	for i := 1; i < len(edits); i++ {
		for j := i; j > 0 && edits[j].Start < edits[j-1].Start; j-- {
			edits[j], edits[j-1] = edits[j-1], edits[j]
		}
	}
}

// Rect is a shape's rectangle in EMU.
type Rect struct{ X, Y, CX, CY int64 }

// Values of an xfrm, read from the current bytes.
func (x *Xfrm) Values(data []byte) (map[string]int64, bool) {
	out := map[string]int64{}
	for k, a := range x.Attrs {
		v, err := strconv.ParseInt(string(data[a.ValStart:a.ValEnd]), 10, 64)
		if err != nil {
			return nil, false
		}
		out[k] = v
	}
	return out, true
}
