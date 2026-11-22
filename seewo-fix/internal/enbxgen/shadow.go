package enbxgen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// A Seewo shape has no picture-style drop shadow. Render the shadow alone,
// behind the editable rectangle, in the original element's animation group.
type shadowLayer struct {
	id                  string
	x, y, width, height float64
}

func (c *conv) rectangleShadow(e *etree.Element, preset string, st shapeStyle, fr frame) (*shadowLayer, error) {
	shadow := e.FindElement("./spPr/effectLst/outerShdw")
	if shadow == nil || preset != "rect" || !strings.HasPrefix(st.fill, "#FF") || fr.flipH || fr.flipV {
		return nil, nil
	}
	// Perspective and scaled shadows require a transformed mask.
	for _, a := range []struct{ name, want string }{{"sx", "100000"}, {"sy", "100000"}, {"kx", "0"}, {"ky", "0"}} {
		if shadow.SelectAttrValue(a.name, a.want) != a.want {
			return nil, nil
		}
	}
	brush, ok := c.d.theme.color(shadow)
	if !ok || !visible(brush) {
		return nil, nil
	}
	argb, err := strconv.ParseUint(strings.TrimPrefix(brush, "#"), 16, 32)
	if err != nil {
		return nil, err
	}
	direction := float64(attrInt(shadow, "dir")) / 60000
	switch shadow.SelectAttrValue("rotWithShape", "1") {
	case "0", "false":
		direction -= fr.rot
	}
	sin, cos := math.Sincos(direction * math.Pi / 180)
	distance := px(attrInt(shadow, "dist"))
	dx, dy := distance*cos, distance*sin
	sigma := max(0, px(attrInt(shadow, "blurRad"))/2)
	halfStroke := st.thickness / 2
	w, h := fr.cx/emuPerPx, fr.cy/emuPerPx
	if w <= 0 || h <= 0 {
		return nil, nil
	}
	pad := math.Ceil(3*sigma + halfStroke + 1)
	width, height := w+2*pad, h+2*pad
	key := "@shadow/" + strings.Join([]string{f(w), f(h), f(halfStroke), f(sigma), brush}, "/") + ".png"
	shadowID := c.res[key].id
	if shadowID == "" {
		// Rectangular Gaussian convolution separates into horizontal and vertical
		// coverage. Bound the raster even for poster-sized slides.
		scale := min(2.0, 1024/math.Max(width, height))
		nx, ny := max(1, int(math.Ceil(width*scale))), max(1, int(math.Ceil(height*scale)))
		cover := func(pos, extent float64) float64 {
			if sigma == 0 {
				if pos >= -halfStroke && pos <= extent+halfStroke {
					return 1
				}
				return 0
			}
			k := sigma * math.Sqrt2
			return (math.Erf((pos+halfStroke)/k) - math.Erf((pos-extent-halfStroke)/k)) / 2
		}
		xCoverage := make([]float64, nx)
		for x := range nx {
			xCoverage[x] = cover((float64(x)+0.5)*width/float64(nx)-pad, w)
		}
		img := image.NewNRGBA(image.Rect(0, 0, nx, ny))
		pixel := color.NRGBA{R: uint8(argb >> 16), G: uint8(argb >> 8), B: uint8(argb)}
		for y := range ny {
			alpha := float64(argb>>24) * cover((float64(y)+0.5)*height/float64(ny)-pad, h)
			for x, cov := range xCoverage {
				pixel.A = uint8(math.Round(alpha * cov))
				img.SetNRGBA(x, y, pixel)
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			return nil, err
		}
		shadowID = c.addResource(key, encoded.Bytes())
	}
	return &shadowLayer{shadowID, dx - pad, dy - pad, width, height}, nil
}

func (c *conv) emitShadow(o *w, s *shadowLayer) {
	if s == nil {
		return
	}
	o.open("Picture")
	o.leaf("Source", "id://"+s.id)
	o.leaf("Alpha", "1")
	o.open("Style")
	o.leaf("StyleType", "None")
	o.close("Style")
	c.tail(o, c.newID("shadow"), s.x, s.y, s.width, s.height, 0, nil, "", nil)
	o.close("Picture")
}
