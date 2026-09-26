package rootfs

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// The andronix wordmark in the same 2-row block font as the terminal
// banner: each character cell is two half-blocks tall.
var markTop = []string{"▄▀█", "█▄ █", "█▀▄", "█▀█", "█▀█", "█▄ █", "█", "▀▄▀"}
var markBot = []string{"█▀█", "█ ▀█", "█▄▀", "█▀▄", "█▄█", "█ ▀█", "█", "█ █"}

// Wallpaper draws the default Andronix desktop background: warm
// near-black with a soft orange glow and the wordmark in an amber to
// deep-orange gradient. Generated, so it costs no download.
func Wallpaper(path string, w, h int) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)*0.46
	maxd := math.Hypot(cx, cy)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy) / maxd
			glow := math.Max(0, 1-d*1.6)
			glow = glow * glow
			// base #15110e, glow toward #3a2210
			r := 21 + glow*37
			g := 17 + glow*17
			b := 14 + glow*2
			img.Set(x, y, color.RGBA{uint8(r), uint8(g), uint8(b), 255})
		}
	}
	// Wordmark geometry: count columns (one per rune, plus a gap).
	cols := 0
	for i, s := range markTop {
		cols += len([]rune(s))
		if i < len(markTop)-1 {
			cols++
		}
	}
	unit := w / 60
	if unit < 4 {
		unit = 4
	}
	mw := cols * unit
	x0 := (w - mw) / 2
	y0 := int(cy) - 2*unit
	grad := [][3]float64{{255, 188, 87}, {255, 139, 37}, {245, 100, 29}, {198, 92, 0}}
	colorAt := func(px int) color.RGBA {
		t := float64(px-x0) / float64(mw)
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}
		seg := t * float64(len(grad)-1)
		i := int(seg)
		if i >= len(grad)-1 {
			i = len(grad) - 2
		}
		f := seg - float64(i)
		a, b := grad[i], grad[i+1]
		return color.RGBA{uint8(a[0] + (b[0]-a[0])*f), uint8(a[1] + (b[1]-a[1])*f), uint8(a[2] + (b[2]-a[2])*f), 255}
	}
	fill := func(col, half int) { // half: 0..3 from the top
		for y := y0 + half*unit; y < y0+(half+1)*unit; y++ {
			for x := x0 + col*unit; x < x0+(col+1)*unit; x++ {
				img.Set(x, y, colorAt(x))
			}
		}
	}
	col := 0
	for i := range markTop {
		t, b := []rune(markTop[i]), []rune(markBot[i])
		for j := range t {
			for row, r := range []rune{t[j], b[j]} {
				switch r {
				case '█':
					fill(col, row*2)
					fill(col, row*2+1)
				case '▀':
					fill(col, row*2)
				case '▄':
					fill(col, row*2+1)
				}
			}
			col++
		}
		col++
	}
	// The orange dot after the x, like the logo.
	dot := color.RGBA{245, 100, 29, 255}
	for y := y0 + 3*unit; y < y0+4*unit; y++ {
		for x := x0 + col*unit; x < x0+(col+1)*unit; x++ {
			img.Set(x, y, dot)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img)
}
