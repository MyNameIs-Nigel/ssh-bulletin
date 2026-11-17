package kit

import (
	"crypto/sha256"
	"image/color"
	"math"
	"strings"
)

// glyphs is a 5×6 pixel font for the wordmark (I is 3 wide). Drawn as
// bitmaps and converted to half-blocks, so the logo can carry a true 2D
// gradient.
var glyphs = map[rune][]string{
	'B': {"####.", "#...#", "####.", "#...#", "#...#", "####."},
	'U': {"#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'L': {"#....", "#....", "#....", "#....", "#....", "#####"},
	'E': {"#####", "#....", "####.", "#....", "#....", "#####"},
	'T': {"#####", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'I': {"###", ".#.", ".#.", ".#.", ".#.", "###"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#"},
}

// Wordmark renders text (A–Z subset above) in the pixel font with a
// diagonal gradient that drifts with phase. The result is 3 rows tall.
//
// There is deliberately no drop shadow: the strokes are one pixel wide, so a
// one-pixel shadow lands inside the letters' counters and reads as broken
// glyphs rather than depth.
func Wordmark(text string, stops []color.Color, phase float64) string {
	const gap, h = 1, 6
	width := 0
	for i, r := range text {
		if i > 0 {
			width += gap
		}
		if g, ok := glyphs[r]; ok {
			width += len(g[0])
		} else {
			width += 2
		}
	}
	px := NewPixels(width, h)
	x0 := 0
	for _, r := range text {
		g, ok := glyphs[r]
		if !ok {
			x0 += 2 + gap
			continue
		}
		for y, row := range g {
			for x, c := range row {
				if c != '#' {
					continue
				}
				gx := x0 + x
				// Diagonal: x dominates, y tilts it ~30°.
				t := (float64(gx)/float64(max(width-1, 1)))*0.85 + float64(y)/float64(h)*0.15
				px.Set(gx, y, Cyclic(stops, t*0.6+phase))
			}
		}
		x0 += len(g[0]) + gap
	}
	return px.Render()
}

// Sigil is a deterministic identicon for a key fingerprint: a 5×5 mirrored
// grid colored along the owner's palette. size 1 draws 5×3 cells (compact,
// for post headers); size 2 draws 10×5 cells (profile cards).
func Sigil(fingerprint string, stops []color.Color, size int) string {
	sum := sha256.Sum256([]byte(fingerprint))
	size = max(1, min(size, 2))
	px := NewPixels(5*size, 5*size)
	for y := range 5 {
		for x := range 3 {
			bit := sum[y*3+x]&1 == 1
			if !bit {
				continue
			}
			t := float64(y)/4*0.8 + float64(x)/2*0.2
			c := At(stops, t)
			for dy := range size {
				for dx := range size {
					px.Set(x*size+dx, y*size+dy, c)
					px.Set((4-x)*size+dx, y*size+dy, c)
				}
			}
		}
	}
	return px.Render()
}

// Orb renders a soft glowing sphere — a radial gradient on the pixel canvas.
// Used as ambient decoration on the splash and onboarding screens. The rim
// fades into bg, which must be the backdrop the orb is drawn over; a darker
// bg leaves a ring of dark pixels around it.
func Orb(radius int, core, edge color.Color, bg color.Color) string {
	// Half-block pixels are roughly square, so a circle is as wide in
	// columns as it is tall in pixels.
	d := radius * 2
	px := NewPixels(d, d)
	for y := range d {
		for x := range d {
			dx := float64(x) - float64(radius) + 0.5
			dy := float64(y) - float64(radius) + 0.5
			r := math.Hypot(dx, dy) / float64(radius)
			if r > 1 {
				continue
			}
			px.Set(x, y, Lerp(Lerp(core, edge, r), bg, math.Pow(r, 3)))
		}
	}
	return px.Render()
}

// Center pads every line of block to width, centered.
func Center(block string, width int) string {
	lines := strings.Split(block, "\n")
	bw := 0
	for _, l := range lines {
		bw = max(bw, visibleWidth(l))
	}
	pad := max(0, (width-bw)/2)
	for i, l := range lines {
		lines[i] = strings.Repeat(" ", pad) + l
	}
	return strings.Join(lines, "\n")
}
