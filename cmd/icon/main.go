// The icon generator: draws DB Studio's product icon as PNGs (256/64/48/32/16)
// plus the multi-size .ico Windows executables embed. 波 100.
//
// The mark: a rounded-square tile in the brand blue, a white data grid of
// three rows and three columns with one cell selected, and a green "run"
// triangle sitting in the selection — the whole product in one glyph: a grid
// of rows, a query, the run. Drawn with Go's image package, no assets.
//
//	go run ./cmd/icon            # writes resources/icon-*.png + resources/icon.ico
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

// brand is the accent the app already paints: mygo light theme's accent blue.
var brand = color.RGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff}
var brandDeep = color.RGBA{R: 0x1d, G: 0x4e, B: 0xd8, A: 0xff}
var ink = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
var cellSoft = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x38}
var runGreen = color.RGBA{R: 0x22, G: 0xc5, B: 0x5e, A: 0xff}

func main() {
	out, err := os.MkdirTemp("", "dbstudio-icon-*")
	if err != nil {
		panic(err)
	}
	_ = out
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	icons := map[int][]byte{}
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, drawIcon(s)); err != nil {
			panic(err)
		}
		icons[s] = buf.Bytes()
	}
	// The resources dir: icon.png (1024-class master, here 256 for repo weight)
	// for docs and stores, the small ones for the window, the .ico for the exe.
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

	// The rounded tile: the app tile, brand blue with a subtle vertical shade.
	radius := 52 * u
	roundRect(img, 0, 0, s, s, radius, brand)
	// Top-light: a soft highlight band gives the tile its depth without a
	// second asset.
	roundRectShade(img, 0, 0, float32(s), float32(s)/2, radius, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x16})
	// The grid: three columns × three rows of white cells, the right column
	// fainter (the data goes on), and margins that keep the tile's edge calm.
	gx, gy := 52*u, 56*u
	cw, ch := 44*u, 30*u
	gapX, gapY := 10*u, 10*u
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			x := gx + float32(col)*(cw+gapX)
			y := gy + float32(row)*(ch+gapY)
			c := ink
			if col == 2 {
				c = cellSoft
			}
			roundRect(img, int(x), int(y), int(x+cw), int(y+ch), 7*u, c)
		}
	}

	// The run triangle in the middle cell: the query, ready. Green pops on
	// both the light and the dark desktop, and reads at 16px.
	cx := gx + cw + gapX
	cy := gy + ch + gapY
	pad := 6 * u
	tri := []image.Point{
		{int(cx + pad), int(cy + pad)},
		{int(cx + pad), int(cy + ch - pad)},
		{int(cx + cw - pad), int(cy + ch/2)},
	}
	fillPoly(img, tri, runGreen)
	return img
}

// roundRect fills an antialiased rounded rectangle via a coverage test per
// pixel. The icon is generated once, so the plain loop is the right cost.
func roundRect(img *image.RGBA, x0, y0, x1, y1 int, r float32, c color.RGBA) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if x < x0 || x >= x1 || y < y0 || y >= y1 {
				continue
			}
			a := coverage(x, y, float32(x0), float32(y0), float32(x1), float32(y1), r)
			if a <= 0 {
				continue
			}
			blendPixel(img, x, y, c, a)
		}
	}
}

// roundRectShade is roundRect over a region clipped to y1 (a band).
func roundRectShade(img *image.RGBA, x0, y0 int, x1, y1, r float32, c color.RGBA) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y && float32(y) < y1; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if x < x0 || float32(x) >= x1 || y < y0 {
				continue
			}
			a := coverage(x, y, float32(x0), float32(y0), x1, y1, r)
			if a <= 0 {
				continue
			}
			blendPixel(img, x, y, c, a)
		}
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
		b := icons[s]
		buf.Write(b)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
