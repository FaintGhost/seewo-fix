// Package enbx reads 希沃白板's own courseware format, which is a zip of
// XML parts, so that a deck can be measured against what 希沃 actually
// produced from it.
//
// Only the geometry is read: each slide's page size and its elements'
// rectangles, keyed by the PowerPoint shape id the converter recorded in
// ElementPptxOriginId. That is enough to measure a conversion exactly.
package enbx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// Element is one top-level element of a slide.
type Element struct {
	Kind   string // Text, Shape, Picture, Table, Group
	Origin string // PowerPoint shape id, "" when the converter made it up
	Name   string
	X, Y   float64 // page units (px at 96 dpi)
	W, H   float64
}

// Slide is one enbx slide.
type Slide struct {
	Index    int // 0-based, from the file name (Slide_0 is the first page)
	Width    float64
	Height   float64
	Elements []Element
	// Children holds the elements nested in a Group, with X/Y relative to it.
	Children map[string][]Element
}

// Doc is one courseware file.
type Doc struct {
	Slides []Slide
}

var slideName = regexp.MustCompile(`^Slides/Slide_(\d+)\.xml$`)

// Read parses a .enbx file.
func Read(data []byte) (*Doc, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open enbx: %w", err)
	}
	doc := &Doc{}
	for _, f := range zr.File {
		m := slideName.FindStringSubmatch(path.Clean(f.Name))
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		s, err := parseSlide(body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		s.Index = idx
		doc.Slides = append(doc.Slides, *s)
	}
	if len(doc.Slides) == 0 {
		return nil, fmt.Errorf("no Slides/Slide_N.xml in the enbx")
	}
	// Slide_N is the page order.
	for i := 1; i < len(doc.Slides); i++ {
		for j := i; j > 0 && doc.Slides[j].Index < doc.Slides[j-1].Index; j-- {
			doc.Slides[j], doc.Slides[j-1] = doc.Slides[j-1], doc.Slides[j]
		}
	}
	return doc, nil
}

func parseSlide(body []byte) (*Slide, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))); err != nil {
		return nil, err
	}
	root := doc.Root()
	if root == nil || root.Tag != "Slide" {
		return nil, fmt.Errorf("not a Slide part")
	}
	s := &Slide{Children: map[string][]Element{}}
	s.Width = num(root.SelectElement("Width"))
	s.Height = num(root.SelectElement("Height"))
	els := root.SelectElement("Elements")
	if els == nil {
		return s, nil
	}
	for _, el := range els.ChildElements() {
		e := elementOf(el)
		s.Elements = append(s.Elements, e)
		if grp := el.SelectElement("Elements"); grp != nil && e.Origin != "" {
			var kids []Element
			for _, k := range grp.ChildElements() {
				kids = append(kids, elementOf(k))
			}
			s.Children[e.Origin] = kids
		}
	}
	return s, nil
}

func elementOf(el *etree.Element) Element {
	e := Element{Kind: el.Tag}
	e.X = num(el.SelectElement("X"))
	e.Y = num(el.SelectElement("Y"))
	e.W = num(el.SelectElement("Width"))
	e.H = num(el.SelectElement("Height"))
	if o := el.SelectElement("ElementPptxOriginId"); o != nil {
		if id := o.SelectElement("OriginPptxId"); id != nil {
			e.Origin = strings.TrimSpace(id.Text())
		}
		if n := o.SelectElement("OriginPptxName"); n != nil {
			e.Name = strings.TrimSpace(n.Text())
		}
	}
	return e
}

func num(el *etree.Element) float64 {
	if el == nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(el.Text()), 64)
	if err != nil {
		return 0
	}
	return v
}
