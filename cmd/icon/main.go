// The icon generator: draws DB Studio's product icon as PNGs (16..256) plus
// the multi-size .ico Windows executables embed. 波 100.
//
// The mark, in the DBeaver/TablePlus family (gradient tile + one bold database
// glyph): a vertically shaded blue rounded tile, the classic database
// cylinder — a stack of discs — in white, and a green "run" triangle on the
// cylinder's face. One glyph says database, one says query. Drawn with Go's
// image package, no assets.
//
//	go run ./cmd/icon            # writes resources/icon*.png + resources/icon.ico
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// brand is the tile's top hue; it shades toward brandDeep at the bottom — the
// gradient every modern database client's icon wears (TablePlus's teal,
// DBeaver's blue, Navicat's green: all one hue, light to dark).
var brand = color.RGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff}     // blue-500
var brandDeep = color.RGBA{R: 0x1e, G: 0x3a, B: 0x8a, A: 0xff} // indigo-900
var ink = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
var inkSoft = color.RGBA{R: 0x1e, G: 0x3a, B: 0x8a, A: 0x66} // a shade of the tile, for seams
var runGreen = color.RGBA{R: 0x4a, G: 0xde, B: 0x80, A: 0xff}

func main() {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	icons := map[int][]byte{}
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, drawIcon(s)); err != nil {
			panic(err)
		}
		icons[s] = buf.Bytes()
	}
	// The resources dir: icon.png (the master, for docs and stores), the small
	// ones for the window/taskbar, the .ico for the exe.
	if err := os.MkdirAll("resources", 0o755); err != nil {
		panic(err)
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join("resources", name), data, 0o644); err != nil {
			panic(err)
		}
		fmt.Printf("resources/%s  %d bytes\n", name, len(data))
	}
	write("icon.png", icons[256])
	write("icon-32.png", icons[32])
	write("icon-16.png", icons[16])
	if err := writeICO("resources/icon.ico", icons); err != nil {
		panic(err)
	}
	fmt.Printf("resources/icon.ico  (16/24/32/48/64/128/256)\n")
}

// drawIcon renders the mark at size s (a square).
func drawIcon(s int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, s, s))
	u := float32(s) / 256 // one design unit at 256

	// The tile: vertical gradient from brand to brandDeep, rounded corners.
	radius := 56 * u
	gradientRoundRect(img, 0, 0, s, s, radius, brand, brandDeep)

	// The database cylinder: ellipse cap, straight sides, rounded bottom, and
	// two seams dividing the body into discs — the database glyph everyone
	// knows. White on the shaded tile.
	cx := float32(s) / 2
	topY := 54 * u
	rx := 72 * u // cylinder's horizontal radius
	ry := 24 * u // ellipse's vertical radius
	h := 116 * u // body height
	cylinder(img, cx, topY, rx, ry, h, ink)

	// Two seams divide the body into thirds; they curve with the rim.
	bodyBottom := topY + ry + h - ry
	for i := 1; i <= 2; i++ {
		seamY := topY + ry + (h-ry)*float32(i)/3
		drawSeam(img, cx, seamY, rx-10*u, (ry-6*u)*0.9, inkSoft)
	}

	// The run triangle on the cylinder's face, right of center: the query,
	// ready to go. Green reads on the blue at every size.
	cx0, cy0 := cx+8*u, topY+ry+30*u
	tri := []image.Point{
		{int(cx0), int(cy0)},
		{int(cx0), int(cy0 + 36*u)},
		{int(cx0 + 30*u), int(cy0 + 18*u)},
	}
	fillPoly(img, tri, runGreen)
	_ = bodyBottom
	return img
}

// cylinder draws a white database: top ellipse, straight sides, bottom curve.
func cylinder(img *image.RGBA, cx, topY, rx, ry, h float32, c color.RGBA) {
	bottomY := topY + h
	bounds := img.Bounds()
	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			fx, fy := float32(px)+0.5, float32(py)+0.5
			in := false
			if fy >= topY-ry && fy <= topY+ry {
				// The top ellipse.
				dy := (fy - topY) / ry
				dx := (fx - cx) / rx
				if dx*dx+dy*dy <= 1 {
					in = true
				}
			}
			if !in && fy > topY && fy < bottomY+ry {
				// The body: inside the horizontal band, closed by the bottom
				// curve below it.
				if fx >= cx-rx && fx <= cx+rx {
					if fy <= bottomY {
						in = true
					} else {
						dy := (fy - bottomY) / ry
						dx := (fx - cx) / rx
						if dx*dx+dy*dy <= 1 {
							in = true
						}
					}
				}
			}
			if in {
				blendPixel(img, px, py, c, 1)
			}
		}
	}
}

// drawSeam strokes a thin elliptical band (the cylinder's seam), its lower
// half only — the seam curves down like the rim does.
func drawSeam(img *image.RGBA, cx, cy, rx, ry float32, c color.RGBA) {
	bounds := img.Bounds()
	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			fx, fy := float32(px)+0.5, float32(py)+0.5
			dx := (fx - cx) / rx
			dy := (fy - cy) / ry
			d := dx*dx + dy*dy
			if d <= 1.10 && d >= 0.72 && fy >= cy {
				blendPixel(img, px, py, c, 1)
			}
		}
	}
}

// gradientRoundRect fills an antialiased rounded rectangle that shades from
// top (c0) to bottom (c1).
func gradientRoundRect(img *image.RGBA, x0, y0, x1, y1 int, r float32, c0, c1 color.RGBA) {
	bounds := img.Bounds()
	h := float32(y1 - y0)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if x < x0 || x >= x1 || y < y0 || y >= y1 {
				continue
			}
			a := coverage(x, y, float32(x0), float32(y0), float32(x1), float32(y1), r)
			if a <= 0 {
				continue
			}
			t := (float32(y) - float32(y0)) / h
			blendPixel(img, x, y, mix(c0, c1, t), a)
		}
	}
}

func mix(a, b color.RGBA, t float32) color.RGBA {
	return color.RGBA{
		R: uint8(float32(a.R) + (float32(b.R)-float32(a.R))*t),
		G: uint8(float32(a.G) + (float32(b.G)-float32(a.G))*t),
		B: uint8(float32(a.B) + (float32(b.B)-float32(a.B))*t),
		A: uint8(float32(a.A) + (float32(b.A)-float32(a.A))*t),
	}
}

// coverage is how much of the pixel (x,y) sits inside the rounded rect, 0..1.
func coverage(px, py int, x0, y0, x1, y1, r float32) float32 {
	const ss = 3 // 3×3 supersampling
	hit := 0
	for sy := 0; ss > sy; sy++ {
		for sx := 0; ss > sx; sx++ {
			fx := float32(px) + (float32(sx)+0.5)/ss
			fy := float32(py) + (float32(sy)+0.5)/ss
			if inRound(fx, fy, x0, y0, x1, y1, r) {
				hit++
			}
		}
	}
	return float32(hit) / (ss * ss)
}

func inRound(fx, fy, x0, y0, x1, y1, r float32) bool {
	// Outside the corner zones the test is the plain rectangle.
	cx := max(min(fx, x1-r), x0+r)
	cy := max(min(fy, y1-r), y0+r)
	dx, dy := fx-cx, fy-cy
	return dx*dx+dy*dy <= r*r
}

func blendPixel(img *image.RGBA, x, y int, c color.RGBA, a float32) {
	i := img.PixOffset(x, y)
	d := img.Pix[i : i+4 : i+4]
	sa := float32(c.A) / 255 * a
	ia := 1 - sa
	d[0] = uint8(float32(d[0])*ia + float32(c.R)*sa)
	d[1] = uint8(float32(d[1])*ia + float32(c.G)*sa)
	d[2] = uint8(float32(d[2])*ia + float32(c.B)*sa)
	d[3] = uint8(max(float32(d[3]), 255*sa))
}

func fillPoly(img *image.RGBA, pts []image.Point, c color.RGBA) {
	minY, maxY := pts[0].Y, pts[0].Y
	minX, maxX := pts[0].X, pts[0].X
	for _, p := range pts[1:] {
		minY = min(minY, p.Y)
		maxY = max(maxY, p.Y)
		minX = min(minX, p.X)
		maxX = max(maxX, p.X)
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if pointInTri(float32(x)+0.5, float32(y)+0.5, pts) {
				blendPixel(img, x, y, c, 1)
			}
		}
	}
}

func pointInTri(px, py float32, pts []image.Point) bool {
	ax, ay := float32(pts[0].X), float32(pts[0].Y)
	bx, by := float32(pts[1].X), float32(pts[1].Y)
	cx, cy := float32(pts[2].X), float32(pts[2].Y)
	d1 := sign(px, py, ax, ay, bx, by)
	d2 := sign(px, py, bx, by, cx, cy)
	d3 := sign(px, py, cx, cy, ax, ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func sign(px, py, ax, ay, bx, by float32) float32 {
	return (px-bx)*(ay-by) - (ax-bx)*(py-by)
}

// writeICO packs the PNGs into one .ico (Vista+ format: PNG payloads directly).
func writeICO(path string, icons map[int][]byte) error {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var buf bytes.Buffer
	// ICONDIR: reserved, type=1 (icon), count.
	buf.Write([]byte{0, 0, 1, 0, byte(len(sizes)), 0})
	offset := uint32(6 + 16*len(sizes))
	for _, s := range sizes {
		b := icons[s]
		w, h := byte(s), byte(s)
		if s >= 256 {
			w, h = 0, 0 // 0 means 256 in the directory entry
		}
		header := []byte{
			w, h, 0, 0, // width, height, palette, reserved
			1, 0, // planes
			32, 0, // bpp
			0, 0, 0, 0, // size patched below
			byte(offset), byte(offset >> 8), byte(offset >> 16), byte(offset >> 24),
		}
		buf.Write(header)
		offset += uint32(len(b))
	}
	for _, s := range sizes {
		buf.Write(icons[s])
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
