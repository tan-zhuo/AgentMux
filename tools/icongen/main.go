// Command icongen draws the AgentMux application icon and writes every size the
// platforms ask for.
//
// The mark is a tmux split: one wide pane in the accent colour with two stacked
// panes beside it. That is literally what the app does, and it survives being
// shrunk to 16 pixels because it is three rectangles and nothing else — no text,
// no thin strokes, no detail that disappears.
//
// Run it from the repository root:
//
//	go run ./tools/icongen
//
// The -iconset flag additionally writes a macOS .iconset directory, which
// `iconutil -c icns` turns into the bundle icon. Every size is drawn from the
// same 1024px master rather than upscaled, so the Retina sizes are crisp:
//
//	go run ./tools/icongen -iconset build/appicon/AgentMux.iconset
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// Canvas is drawn once at this size, then downsampled. Supersampling is what
// gives the rounded corners clean edges without a rasterising library.
const master = 1024

var (
	bgTop    = color.NRGBA{0x16, 0x1b, 0x26, 0xff}
	bgBottom = color.NRGBA{0x08, 0x0a, 0x0f, 0xff}
	rim      = color.NRGBA{0x2b, 0x33, 0x44, 0xff}
	accent   = color.NRGBA{0x4c, 0x8d, 0xff, 0xff}
	slate    = color.NRGBA{0x33, 0x3d, 0x52, 0xff}
	live     = color.NRGBA{0x3d, 0xdc, 0x97, 0xff}
)

// rrect is a rounded rectangle in canvas coordinates.
type rrect struct{ x0, y0, x1, y1, r float64 }

// covers reports whether a point is inside the rounded rectangle.
func (b rrect) covers(x, y float64) bool {
	if x < b.x0 || x > b.x1 || y < b.y0 || y > b.y1 {
		return false
	}
	// Only the corner quadrants need the distance test.
	cx, cy := x, y
	switch {
	case x < b.x0+b.r:
		cx = b.x0 + b.r
	case x > b.x1-b.r:
		cx = b.x1 - b.r
	default:
		return true
	}
	switch {
	case y < b.y0+b.r:
		cy = b.y0 + b.r
	case y > b.y1-b.r:
		cy = b.y1 - b.r
	default:
		return true
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= b.r*b.r
}

type circle struct{ cx, cy, r float64 }

func (c circle) covers(x, y float64) bool {
	dx, dy := x-c.cx, y-c.cy
	return dx*dx+dy*dy <= c.r*c.r
}

type shape interface{ covers(x, y float64) bool }

// paint composites a shape onto the image with 4x4 supersampled coverage.
func paint(img *image.NRGBA, s shape, col color.NRGBA) {
	const ss = 4
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			hits := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					if s.covers(px, py) {
						hits++
					}
				}
			}
			if hits == 0 {
				continue
			}
			a := float64(hits) / float64(ss*ss)
			blend(img, x, y, col, a)
		}
	}
}

func blend(img *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	i := img.PixOffset(x, y)
	dst := color.NRGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	srcA := a * float64(c.A) / 255
	outA := srcA + float64(dst.A)/255*(1-srcA)
	if outA <= 0 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0, 0, 0, 0
		return
	}
	mix := func(s, d uint8) uint8 {
		v := (float64(s)*srcA + float64(d)/255*float64(dst.A)/255*(1-srcA)*255) / outA
		return uint8(math.Round(math.Min(255, math.Max(0, v))))
	}
	img.Pix[i] = mix(c.R, dst.R)
	img.Pix[i+1] = mix(c.G, dst.G)
	img.Pix[i+2] = mix(c.B, dst.B)
	img.Pix[i+3] = uint8(math.Round(outA * 255))
}

func drawIcon() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, master, master))

	// Body: a rounded square with a top-to-bottom gradient, the shape every
	// platform expects an app icon to be.
	body := rrect{0, 0, master, master, master * 0.22}
	for y := 0; y < master; y++ {
		t := float64(y) / float64(master-1)
		row := color.NRGBA{
			R: uint8(float64(bgTop.R) + (float64(bgBottom.R)-float64(bgTop.R))*t),
			G: uint8(float64(bgTop.G) + (float64(bgBottom.G)-float64(bgTop.G))*t),
			B: uint8(float64(bgTop.B) + (float64(bgBottom.B)-float64(bgTop.B))*t),
			A: 255,
		}
		paint(img, rrect{0, float64(y), master, float64(y + 1), 0}, row)
	}
	// Clip the gradient to the rounded body by erasing everything outside it.
	clip := image.NewNRGBA(img.Bounds())
	paint(clip, body, color.NRGBA{255, 255, 255, 255})
	for i := 0; i < len(img.Pix); i += 4 {
		a := float64(clip.Pix[i+3]) / 255
		img.Pix[i+3] = uint8(float64(img.Pix[i+3]) * a)
	}

	// A hairline rim lifts the icon off dark docks and taskbars.
	paint(img, ringShape{outer: body, inset: master * 0.012}, rim)

	// The panes. Gaps between them are the surface showing through, which is
	// what makes three rectangles read as a split rather than one block.
	const (
		left   = 232.0
		right  = 792.0
		top    = 268.0
		bottom = 756.0
		gap    = 44.0
		radius = 44.0
	)
	split := left + (right-left)*0.50
	midY := top + (bottom-top)*0.5

	paint(img, rrect{left, top, split - gap/2, bottom, radius}, accent)
	paint(img, rrect{split + gap/2, top, right, midY - gap/2, radius}, slate)
	paint(img, rrect{split + gap/2, midY + gap/2, right, bottom, radius}, slate)

	// One running agent, in the pane that is doing the work.
	paint(img, circle{left + 74, top + 74, 30}, live)

	return img
}

// ringShape is the area between a rounded rect and an inset copy of itself.
type ringShape struct {
	outer rrect
	inset float64
}

func (r ringShape) covers(x, y float64) bool {
	inner := rrect{
		r.outer.x0 + r.inset, r.outer.y0 + r.inset,
		r.outer.x1 - r.inset, r.outer.y1 - r.inset,
		math.Max(0, r.outer.r-r.inset),
	}
	return r.outer.covers(x, y) && !inner.covers(x, y)
}

// resize does a box-filter downsample, which is the right filter when going
// from one large master to small icon sizes.
func resize(src *image.NRGBA, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := float64(src.Bounds().Dx()) / float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0, x1 := int(float64(x)*scale), int(float64(x+1)*scale)
			y0, y1 := int(float64(y)*scale), int(float64(y+1)*scale)
			var r, g, b, a, n float64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					i := src.PixOffset(sx, sy)
					pa := float64(src.Pix[i+3]) / 255
					r += float64(src.Pix[i]) * pa
					g += float64(src.Pix[i+1]) * pa
					b += float64(src.Pix[i+2]) * pa
					a += pa
					n++
				}
			}
			if n == 0 {
				continue
			}
			j := dst.PixOffset(x, y)
			if a > 0 {
				dst.Pix[j] = uint8(math.Round(r / a))
				dst.Pix[j+1] = uint8(math.Round(g / a))
				dst.Pix[j+2] = uint8(math.Round(b / a))
			}
			dst.Pix[j+3] = uint8(math.Round(a / n * 255))
		}
	}
	return dst
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// dibPayload encodes one icon image as a bottom-up 32-bit BGRA DIB with the
// trailing AND mask an .ico entry expects.
//
// Small entries must be DIB rather than PNG. PNG payloads are legal from Vista
// onwards but only reliably honoured at 256px; several shell paths — and
// System.Drawing, which is what creates the shortcut — read a small PNG entry as
// raw DIB bytes and render noise.
func dibPayload(im *image.NRGBA) []byte {
	w := im.Bounds().Dx()
	h := im.Bounds().Dy()

	// AND mask rows are 1 bit per pixel, padded to a 4-byte boundary.
	maskStride := ((w + 31) / 32) * 4
	var buf bytes.Buffer

	// BITMAPINFOHEADER. The height covers both the colour data and the mask.
	_ = binary.Write(&buf, binary.LittleEndian, uint32(40))
	_ = binary.Write(&buf, binary.LittleEndian, int32(w))
	_ = binary.Write(&buf, binary.LittleEndian, int32(h*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(32))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // BI_RGB
	_ = binary.Write(&buf, binary.LittleEndian, uint32(w*h*4+maskStride*h))
	_ = binary.Write(&buf, binary.LittleEndian, int32(0))
	_ = binary.Write(&buf, binary.LittleEndian, int32(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))

	// Colour data, bottom-up, BGRA with premultiplied-looking straight alpha.
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			i := im.PixOffset(x, y)
			buf.WriteByte(im.Pix[i+2]) // B
			buf.WriteByte(im.Pix[i+1]) // G
			buf.WriteByte(im.Pix[i+0]) // R
			buf.WriteByte(im.Pix[i+3]) // A
		}
	}
	// AND mask: all zero, meaning "use the alpha channel".
	buf.Write(make([]byte, maskStride*h))
	return buf.Bytes()
}

// writeICO packs images into a Windows .ico, choosing the payload format each
// size is actually read correctly in.
func writeICO(path string, images []*image.NRGBA) error {
	var payloads [][]byte
	for _, im := range images {
		if im.Bounds().Dx() >= 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, im); err != nil {
				return err
			}
			payloads = append(payloads, buf.Bytes())
			continue
		}
		payloads = append(payloads, dibPayload(im))
	}

	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(images)))

	offset := 6 + 16*len(images)
	for i, im := range images {
		size := im.Bounds().Dx()
		dim := byte(size)
		if size >= 256 {
			dim = 0 // 0 means 256 in the ICO directory
		}
		out.WriteByte(dim)
		out.WriteByte(dim)
		out.WriteByte(0)                                        // palette size
		out.WriteByte(0)                                        // reserved
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // planes
		_ = binary.Write(&out, binary.LittleEndian, uint16(32)) // bits per pixel
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(payloads[i])))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(payloads[i])
	}
	for _, p := range payloads {
		out.Write(p)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func main() {
	iconset := flag.String("iconset", "", "also write a macOS .iconset directory here")
	flag.Parse()

	outDir := filepath.Join("build", "appicon")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}

	master := drawIcon()

	// A flattened copy for places that cannot handle transparency well.
	flat := image.NewNRGBA(master.Bounds())
	draw.Draw(flat, flat.Bounds(), &image.Uniform{color.NRGBA{0, 0, 0, 0}}, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), master, image.Point{}, draw.Over)

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var icoImages []*image.NRGBA
	for _, s := range sizes {
		im := resize(master, s)
		icoImages = append(icoImages, im)
		if s == 256 || s == 128 {
			if err := writePNG(filepath.Join(outDir, fmt.Sprintf("icon-%d.png", s)), im); err != nil {
				panic(err)
			}
		}
	}
	if err := writePNG(filepath.Join(outDir, "icon.png"), resize(master, 512)); err != nil {
		panic(err)
	}
	if err := writeICO(filepath.Join(outDir, "icon.ico"), icoImages); err != nil {
		panic(err)
	}

	fmt.Printf("wrote %s: icon.ico (%d sizes), icon.png, icon-256.png, icon-128.png\n", outDir, len(sizes))

	if *iconset != "" {
		if err := writeIconset(*iconset, master); err != nil {
			panic(err)
		}
		fmt.Printf("wrote %s\n", *iconset)
	}
}

// writeIconset lays out the file names iconutil expects. The @2x entries are
// the next size up under a different name, which is what Apple's tooling wants
// — not an upscaled copy of the 1x file. Every one is drawn from the same
// 1024px master, so nothing here is a blur.
func writeIconset(dir string, src *image.NRGBA) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := map[string]int{
		"icon_16x16.png": 16, "icon_16x16@2x.png": 32,
		"icon_32x32.png": 32, "icon_32x32@2x.png": 64,
		"icon_128x128.png": 128, "icon_128x128@2x.png": 256,
		"icon_256x256.png": 256, "icon_256x256@2x.png": 512,
		"icon_512x512.png": 512, "icon_512x512@2x.png": 1024,
	}
	for name, size := range names {
		if err := writePNG(filepath.Join(dir, name), resize(src, size)); err != nil {
			return err
		}
	}
	return nil
}
