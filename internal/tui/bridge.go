package tui

import (
	_ "embed"
	"image/color"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-bulletin/internal/tui/kit"
)

// The splash scene: a suspension bridge over moving water with birds
// crossing the sky. The artwork is bridge.txt, by "jgs".
//
// TODO(attribution): the "jgs" tag is drawn bold and underlined as a stand-in
// for a link to bridgeSource. It is not clickable yet.
//
//go:embed bridge.txt
var bridgeTxt string

const (
	bridgeTag    = "jgs"
	bridgeSource = "https://www.asciiart.eu/art/e0c8b6243467d839"
)

// Motion is quantized: identical frames cost nothing on the wire, and the
// scene is a pure function of the elapsed time, so renders are deterministic.
const (
	waterStep = 0.35 // seconds per one-cell shift of a water row
	flapStep  = 0.30 // seconds per wing beat
)

// birds are sprites, not part of the art. lane is a row of the displayed
// scene; step is the seconds a bird takes to cross one column.
var birds = [...]struct {
	lane  int
	step  float64
	phase float64 // where on the loop it starts, 0..1
}{
	{0, 0.11, 0.15},
	{1, 0.16, 0.55},
	{3, 0.21, 0.85},
}

// bridgeArt is the parsed artwork.
type bridgeArt struct {
	rows       [][]rune
	w          int
	waterStart int     // first water row
	feet       [][]int // per row: columns of the pier feet "/  \", never animated
	cycle      [][]int // per row: columns whose glyphs flow with the water
	tagRow     int
}

// dropOrder lists rows to give up, in order, when the terminal is short.
// The signature's row is never on it: the attribution has to stay visible.
var dropOrder = []int{20, 19, 13, 12, 0, 1, 17, 16}

var (
	bridgeOnce sync.Once
	bridge     *bridgeArt
)

func theBridge() *bridgeArt {
	bridgeOnce.Do(func() { bridge = parseBridge(bridgeTxt) })
	return bridge
}

func parseBridge(src string) *bridgeArt {
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	b := &bridgeArt{waterStart: -1, tagRow: -1}
	for _, l := range lines {
		b.w = max(b.w, len([]rune(l)))
	}
	for i, l := range lines {
		// Pad every row to the same width: the scene is centered as one
		// block, and centering rows one by one would shear the art.
		r := []rune(l + strings.Repeat(" ", b.w-len([]rune(l))))
		b.rows = append(b.rows, r)
		var feet []int
		for from := 0; ; {
			j := strings.Index(l[from:], "/  \\")
			if j < 0 {
				break
			}
			j += from
			feet = append(feet, j, j+1, j+2, j+3)
			from = j + 4
		}
		b.feet = append(b.feet, feet)
		if len(feet) > 0 && b.waterStart < 0 {
			b.waterStart = i
		}
		if strings.HasPrefix(l, bridgeTag) {
			b.tagRow = i
		}
	}
	// Piers and the signature never move.
	b.cycle = make([][]int, len(b.rows))
	for i := b.waterStart; i >= 0 && i < len(b.rows); i++ {
		fixed := map[int]bool{}
		for _, c := range b.feet[i] {
			fixed[c] = true
		}
		if i == b.tagRow {
			for c := range len(bridgeTag) {
				fixed[c] = true
			}
		}
		// Only glyphs flow, along the glyph positions: every space stays a
		// space, so the outline and the gaps in the waves hold still.
		for c, r := range b.rows[i] {
			if r != ' ' && !fixed[c] {
				b.cycle[i] = append(b.cycle[i], c)
			}
		}
	}
	return b
}

// fit returns the rows to show in budget terminal rows, or nil when even the
// smallest form of the scene does not fit.
func (b *bridgeArt) fit(budget int) []int {
	keep := make([]bool, len(b.rows))
	n := len(b.rows)
	for i := range keep {
		keep[i] = true
	}
	for _, d := range dropOrder {
		if n <= budget {
			break
		}
		keep[d] = false
		n--
	}
	if n > budget {
		return nil
	}
	var rows []int
	for i, k := range keep {
		if k {
			rows = append(rows, i)
		}
	}
	return rows
}

type inkKey struct {
	r, g, b, a uint32
	ul         bool
}

type cellInk struct {
	c  color.Color
	ul bool // bold + underline: the signature
}

func (c cellInk) key() inkKey {
	r, g, b, a := c.c.RGBA()
	return inkKey{r, g, b, a, c.ul}
}

// render draws rows of the scene at time t seconds, full terminal width so
// the birds can cross the whole screen with the bridge centered under them.
func (b *bridgeArt) render(rows []int, w int, t float64, th kit.Theme) string {
	stops := th.Brand.Stops
	x0 := (w - b.w) / 2
	waterRows := len(b.rows) - b.waterStart
	teal := kit.At(stops, 0.66)
	birdInk := kit.At(stops, 1)
	tagInk := kit.At(stops, 0)
	beat := int(t/flapStep) % 2
	wing := [2]string{"^^", "vv"}[beat]

	out := make([]string, 0, len(rows))
	for dr, ri := range rows {
		line := make([]rune, w)
		ink := make([]cellInk, w)
		for x := range line {
			line[x] = ' '
		}
		src := b.rows[ri]
		water := b.waterStart >= 0 && ri >= b.waterStart

		glyphs := append([]rune(nil), src...)
		if water {
			wr := ri - b.waterStart
			cyc := b.cycle[ri]
			if n := len(cyc); n > 1 {
				speed := [3]float64{1, 0.7, 1.3}[wr%3]
				shift := int(t / waterStep * speed)
				if wr%2 == 1 {
					shift = -shift
				}
				shift = ((shift % n) + n) % n
				for i, c := range cyc {
					glyphs[c] = src[cyc[(i+shift)%n]]
				}
			}
		}

		depth := 0.0
		if water {
			depth = float64(ri-b.waterStart) / float64(max(1, waterRows-1))
		}
		for ac, r := range glyphs {
			x := x0 + ac
			if r == ' ' || x < 0 || x >= w {
				continue
			}
			line[x] = r
			switch {
			case water && ri == b.tagRow && ac < len(bridgeTag):
				ink[x] = cellInk{c: tagInk, ul: true}
			case water && isFoot(b.feet[ri], ac):
				ink[x] = cellInk{c: th.Subtle}
			case water:
				ink[x] = cellInk{c: kit.Lerp(teal, th.Muted, depth*0.6)}
			case r == '[' || r == ']':
				ink[x] = cellInk{c: kit.Lerp(kit.At(stops, float64(ac)/float64(b.w)), th.Text, 0.3)}
			default:
				ink[x] = cellInk{c: kit.Lerp(kit.At(stops, float64(ac)/float64(b.w)), th.Muted, 0.3)}
			}
		}

		// Birds fly over empty space only; the art passes in front of them.
		for _, bd := range birds {
			if bd.lane != dr {
				continue
			}
			period := w + 4
			x := (int(t/bd.step)+int(bd.phase*float64(period)))%period - 2
			for k, wr := range wing {
				if cx := x + k; cx >= 0 && cx < w && line[cx] == ' ' {
					line[cx] = wr
					ink[cx] = cellInk{c: birdInk}
				}
			}
		}
		out = append(out, paintRuns(line, ink))
	}
	return strings.Join(out, "\n")
}

func isFoot(feet []int, c int) bool {
	for _, f := range feet {
		if f == c {
			return true
		}
	}
	return false
}

// paintRuns styles a row, one escape sequence per run of identical ink.
func paintRuns(line []rune, ink []cellInk) string {
	var sb strings.Builder
	for i := 0; i < len(line); {
		if ink[i].c == nil {
			j := i
			for j < len(line) && ink[j].c == nil {
				j++
			}
			sb.WriteString(string(line[i:j]))
			i = j
			continue
		}
		j := i
		for j < len(line) && ink[j].c != nil && ink[j].key() == ink[i].key() {
			j++
		}
		st := lipgloss.NewStyle().Foreground(ink[i].c)
		if ink[i].ul {
			st = st.Bold(true).Underline(true)
		}
		sb.WriteString(st.Render(string(line[i:j])))
		i = j
	}
	return sb.String()
}
