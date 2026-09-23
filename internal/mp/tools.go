package mp

import (
	"encoding/json"
	"fmt"
	"math"
)

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// OpKind names a drawing primitive.
type OpKind string

const (
	OpPencil  OpKind = "pencil"  // freehand polyline through Points
	OpLine    OpKind = "line"    // straight line, Points[0] to Points[1]
	OpRect    OpKind = "rect"    // rectangle bounded by Points[0], Points[1]
	OpEllipse OpKind = "ellipse" // ellipse inscribed in the same bounds
	OpFill    OpKind = "fill"    // flood fill from Points[0]
	OpSpray   OpKind = "spray"   // spray can along Points
)

// Op is one drawing action. Coordinates are in visible-canvas space, so
// (0,0) is the top-left pixel the player can actually see.
type Op struct {
	Kind    OpKind  `json:"kind"`
	Points  []Point `json:"points"`
	Color   byte    `json:"color"`
	Size    int     `json:"size,omitempty"`
	Filled  bool    `json:"filled,omitempty"`
	Density int     `json:"density,omitempty"` // spray dots per step
}

// Apply draws op onto c.
//
// emit is called as the drawing advances, with the position the cursor has
// reached. Passing a non-nil emit is what turns a one-shot edit into a
// timelapse: the caller advances a frame each time it fires.
func Apply(c *Canvas, op Op, emit func(x, y int)) {
	if emit == nil {
		emit = func(int, int) {}
	}
	size := op.Size
	if size < 1 {
		size = 1
	}

	switch op.Kind {
	case OpPencil:
		polyline(c, op.Points, op.Color, size, emit)
	case OpLine:
		if len(op.Points) >= 2 {
			polyline(c, op.Points[:2], op.Color, size, emit)
		}
	case OpRect:
		if len(op.Points) >= 2 {
			rect(c, op.Points[0], op.Points[1], op.Color, size, op.Filled, emit)
		}
	case OpEllipse:
		if len(op.Points) >= 2 {
			ellipse(c, op.Points[0], op.Points[1], op.Color, size, op.Filled, emit)
		}
	case OpFill:
		if len(op.Points) >= 1 {
			floodFill(c, op.Points[0], op.Color, emit)
		}
	case OpSpray:
		spray(c, op.Points, op.Color, size, op.Density, emit)
	}
}

// brush stamps a round dab of the given diameter centred on (x, y).
func brush(c *Canvas, x, y int, color byte, size int) {
	if size <= 1 {
		c.Set(VisibleX+x, VisibleY+y, color)
		return
	}
	r := float64(size) / 2
	off := (size - 1) / 2
	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			fx, fy := float64(dx)-r+0.5, float64(dy)-r+0.5
			if fx*fx+fy*fy > r*r {
				continue
			}
			c.Set(VisibleX+x-off+dx, VisibleY+y-off+dy, color)
		}
	}
}

func inVisible(p Point) bool {
	return p.X >= 0 && p.Y >= 0 && p.X < VisibleW && p.Y < VisibleH
}

// bresenham walks the integer line from a to b, calling fn for every pixel.
func bresenham(a, b Point, fn func(x, y int)) {
	dx, sx := abs(b.X-a.X), sign(b.X-a.X)
	dy, sy := -abs(b.Y-a.Y), sign(b.Y-a.Y)
	err := dx + dy
	x, y := a.X, a.Y
	for {
		fn(x, y)
		if x == b.X && y == b.Y {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
}

func polyline(c *Canvas, pts []Point, color byte, size int, emit func(x, y int)) {
	if len(pts) == 0 {
		return
	}
	if len(pts) == 1 {
		brush(c, pts[0].X, pts[0].Y, color, size)
		emit(pts[0].X, pts[0].Y)
		return
	}
	for i := 0; i+1 < len(pts); i++ {
		bresenham(pts[i], pts[i+1], func(x, y int) {
			brush(c, x, y, color, size)
			emit(x, y)
		})
	}
}

func rect(c *Canvas, a, b Point, color byte, size int, filled bool, emit func(x, y int)) {
	x0, x1 := minMax(a.X, b.X)
	y0, y1 := minMax(a.Y, b.Y)
	if filled {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				c.Set(VisibleX+x, VisibleY+y, color)
			}
			emit(x1, y)
		}
		return
	}
	polyline(c, []Point{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}, {x0, y0}}, color, size, emit)
}

func ellipse(c *Canvas, a, b Point, color byte, size int, filled bool, emit func(x, y int)) {
	x0, x1 := minMax(a.X, b.X)
	y0, y1 := minMax(a.Y, b.Y)
	cx, cy := float64(x0+x1)/2, float64(y0+y1)/2
	rx, ry := float64(x1-x0)/2, float64(y1-y0)/2
	if rx < 0.5 || ry < 0.5 {
		polyline(c, []Point{{x0, y0}, {x1, y1}}, color, size, emit)
		return
	}

	if filled {
		for y := y0; y <= y1; y++ {
			ny := (float64(y) - cy) / ry
			if ny*ny > 1 {
				continue
			}
			half := rx * math.Sqrt(1-ny*ny)
			for x := int(cx - half); x <= int(cx+half); x++ {
				c.Set(VisibleX+x, VisibleY+y, color)
			}
			emit(int(cx+half), y)
		}
		return
	}

	// Walk the perimeter by angle; step fine enough that no pixel is skipped.
	steps := int(2*math.Pi*math.Max(rx, ry)) + 8
	for i := 0; i <= steps; i++ {
		t := 2 * math.Pi * float64(i) / float64(steps)
		x := int(cx + rx*math.Cos(t) + 0.5)
		y := int(cy + ry*math.Sin(t) + 0.5)
		brush(c, x, y, color, size)
		emit(x, y)
	}
}

func floodFill(c *Canvas, seed Point, color byte, emit func(x, y int)) {
	if !inVisible(seed) {
		return
	}
	target := c.At(VisibleX+seed.X, VisibleY+seed.Y)
	if target == color {
		return
	}

	// Scanline flood fill: far fewer emits than a per-pixel stack, which also
	// makes the timelapse read as a fill sweeping across rather than crawling.
	stack := []Point{seed}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !inVisible(p) || c.At(VisibleX+p.X, VisibleY+p.Y) != target {
			continue
		}

		left, right := p.X, p.X
		for left > 0 && c.At(VisibleX+left-1, VisibleY+p.Y) == target {
			left--
		}
		for right < VisibleW-1 && c.At(VisibleX+right+1, VisibleY+p.Y) == target {
			right++
		}
		for x := left; x <= right; x++ {
			c.Set(VisibleX+x, VisibleY+p.Y, color)
		}
		emit((left+right)/2, p.Y)

		for x := left; x <= right; x++ {
			if p.Y > 0 && c.At(VisibleX+x, VisibleY+p.Y-1) == target {
				stack = append(stack, Point{x, p.Y - 1})
			}
			if p.Y < VisibleH-1 && c.At(VisibleX+x, VisibleY+p.Y+1) == target {
				stack = append(stack, Point{x, p.Y + 1})
			}
		}
	}
}

func spray(c *Canvas, pts []Point, color byte, size, density int, emit func(x, y int)) {
	if density < 1 {
		density = 8
	}
	r := float64(size) * 2
	// Deterministic scatter: a fixed LCG keeps renders reproducible, which
	// matters when the same session is re-rendered into a video.
	var seed uint32 = 0x2545F491
	next := func() float64 {
		seed = seed*1664525 + 1013904223
		return float64(seed>>8) / float64(1<<24)
	}

	walk := pts
	if len(walk) == 1 {
		walk = append(walk, pts[0])
	}
	for i := 0; i+1 < len(walk); i++ {
		bresenham(walk[i], walk[i+1], func(x, y int) {
			for d := 0; d < density; d++ {
				ang := next() * 2 * math.Pi
				rad := math.Sqrt(next()) * r
				c.Set(VisibleX+x+int(rad*math.Cos(ang)), VisibleY+y+int(rad*math.Sin(ang)), color)
			}
			emit(x, y)
		})
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func minMax(a, b int) (int, int) {
	if a > b {
		return b, a
	}
	return a, b
}

// CanvasToOps turns a finished canvas into the strokes that would produce it:
// each row becomes a run of same-coloured line segments.
//
// Replaying these is what makes an imported photograph appear as something
// Mario Paint drew, rather than something pasted into it. Background-coloured
// runs are skipped, since the canvas already starts that colour.
func CanvasToOps(c *Canvas) []Op {
	var ops []Op
	for y := 0; y < VisibleH; y++ {
		x := 0
		for x < VisibleW {
			col := c.At(VisibleX+x, VisibleY+y)
			run := x
			for run < VisibleW && c.At(VisibleX+run, VisibleY+y) == col {
				run++
			}
			if col != 0 {
				ops = append(ops, Op{
					Kind:   OpLine,
					Points: []Point{{x, y}, {run - 1, y}},
					Color:  col,
					Size:   1,
				})
			}
			x = run
		}
	}
	return ops
}

// UnmarshalJSON accepts a colour written either as a palette index or by
// name, so a hand-written operations file can say "red" rather than 1.
func (o *Op) UnmarshalJSON(data []byte) error {
	type plain Op
	var raw struct {
		plain
		Color json.RawMessage `json:"color"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*o = Op(raw.plain)

	if len(raw.Color) == 0 || string(raw.Color) == "null" {
		return nil
	}
	var name string
	if err := json.Unmarshal(raw.Color, &name); err == nil {
		idx, err := ParseColor(name)
		if err != nil {
			return err
		}
		o.Color = idx
		return nil
	}
	var idx byte
	if err := json.Unmarshal(raw.Color, &idx); err != nil {
		return fmt.Errorf("operation colour: want a palette name or an index 0..15")
	}
	if idx > 15 {
		return fmt.Errorf("palette index %d is out of range (0..15)", idx)
	}
	o.Color = idx
	return nil
}
