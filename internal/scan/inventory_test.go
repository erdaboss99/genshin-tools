package scan

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
	"testing"

	"github.com/erdaboss99/genshin-tools/control"
	"github.com/erdaboss99/genshin-tools/internal/layout"
)

func TestParseItemCount(t *testing.T) {
	tests := []struct {
		text string
		want int
		ok   bool
	}{
		{"Artifacts 1234/2100", 1234, true},
		{"1234/2100", 1234, true},
		{"1234 / 2100", 1234, true},
		{"12O4/2100", 1204, true},
		{"12&4/2100", 1284, true},
		{"", 0, false},
		{"Artifacts", 0, false},
		{"0/2100", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseItemCount(tt.text)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseItemCount(%q) = %d, %v; want %d, %v", tt.text, got, ok, tt.want, tt.ok)
		}
	}
}

// simulatedInventory models the inventory grid well enough to check the traversal:
// the mouse wheel moves the list, clamped at its end, and a click selects whatever
// is under the cursor.
type simulatedInventory struct {
	width, height int
	grid          layout.Grid
	panel         layout.Box
	artifacts     int
	materials     int
	failAfter     int
	stuckClicks   bool
	// stack makes the items from stack[0] up to stack[1] identical copies.
	stack [2]int
	// reuseFrame shares one image between captures, for tests that consume each
	// frame before capturing the next.
	reuseFrame bool
	frame      *image.RGBA

	ticks    int
	clicks   int
	captures int
	current  int
	visited  []int
}

func (s *simulatedInventory) win() layout.Window {
	win, err := layout.NewWindow(s.width, s.height)
	if err != nil {
		panic(err)
	}
	return win
}

func (s *simulatedInventory) totalRows() int {
	return (s.artifacts+s.materials-1)/s.grid.Columns + 1
}

func (s *simulatedInventory) topRow() (row int, atBottom bool) {
	win := s.win()
	limit := max(s.totalRows()-s.grid.RowsPerPage(win), 0)
	row = int(float64(-s.ticks)/s.grid.ScrollTicksPerRow + 0.5)
	if row >= limit {
		return limit, true
	}
	return row, false
}

func (s *simulatedInventory) Size() (int, int) { return s.width, s.height }

func (s *simulatedInventory) MoveTo(int, int) error { return nil }

// Scroll stops at both ends of the list, as the game does, so scrolling back up after
// overshooting the bottom starts from the bottom, not from where the overshoot ended.
func (s *simulatedInventory) Scroll(ticks int) error {
	s.ticks += ticks
	limit := max(s.totalRows()-s.grid.RowsPerPage(s.win()), 0)
	s.ticks = min(max(s.ticks, -int(math.Ceil(float64(limit)*s.grid.ScrollTicksPerRow))), 0)
	return nil
}

func (s *simulatedInventory) Click(x, y int) error {
	s.clicks++
	if s.failAfter > 0 && s.clicks > s.failAfter {
		return control.ErrFocusLost
	}
	if s.stuckClicks {
		return nil
	}
	win := s.win()
	col := (x - win.X(s.grid.First.X) + win.Len(s.grid.DeltaX)/2) / win.Len(s.grid.DeltaX)
	var row int
	if top, atBottom := s.topRow(); atBottom && s.totalRows() > s.grid.RowsPerPage(win) {
		fromBottom := (win.Y(s.grid.LastRowY) - y + win.Len(s.grid.DeltaY)/2) / win.Len(s.grid.DeltaY)
		row = s.totalRows() - 1 - fromBottom
	} else {
		row = top + (y-win.Y(s.grid.First.Y)+win.Len(s.grid.DeltaY)/2)/win.Len(s.grid.DeltaY)
	}
	// Clicking an empty cell past the last item leaves the selection as it was.
	if item := row*s.grid.Columns + col; item < s.artifacts+s.materials {
		s.current = item
	}
	return nil
}

func (s *simulatedInventory) Capture() (*image.RGBA, error) {
	img := s.frame
	if img == nil || !s.reuseFrame {
		img = image.NewRGBA(image.Rect(0, 0, s.width, s.height))
		s.frame = img
	}
	// Something in the grid animates, as in the game, so the grid never looks the
	// same twice and can't tell whether the list moved.
	s.captures++
	img.SetRGBA(s.win().X(s.grid.First.X), s.win().Y(s.grid.First.Y), color.RGBA{uint8(s.captures >> 8), uint8(s.captures), 3, 255})
	id := s.panelID(s.current) + 1
	item := color.RGBA{uint8(id >> 8), uint8(id), 7, 255}
	draw.Draw(img, s.win().Rect(s.panel), image.NewUniform(item), image.Point{}, draw.Src)
	return img, nil
}

// panelID is what the panel shows for item: the item itself, or the first item of
// its stack of identical copies.
func (s *simulatedInventory) panelID(item int) int {
	if item >= s.stack[0] && item < s.stack[1] {
		return s.stack[0]
	}
	return item
}

func (s *simulatedInventory) Type(string) error { return nil }

func (s *simulatedInventory) PressEnter() error { return nil }

func (s *simulatedInventory) PressEscape() error { return nil }

func (s *simulatedInventory) itemIn(frame *image.RGBA) int {
	at := s.win().Rect(s.panel).Min
	c := frame.RGBAAt(at.X+1, at.Y+1)
	return int(c.R)<<8 | int(c.G) - 1
}

func (s *simulatedInventory) walker() InventoryWalker {
	return InventoryWalker{
		Game:   s,
		Layout: layout.Browse{Grid: s.grid, Panel: s.panel},
		Count:  s.artifacts,
		IsMaterial: func(_ context.Context, frame *image.RGBA) (bool, error) {
			return s.itemIn(frame) >= s.artifacts, nil
		},
	}
}

func TestInventoryWalkVisitsEveryArtifactExactlyOnce(t *testing.T) {
	type config struct {
		size                 [2]int
		artifacts, materials int
	}
	var configs []config
	for _, size := range [][2]int{{1280, 720}, {1280, 800}} {
		for _, materials := range []int{0, 3, 9} {
			for _, artifacts := range []int{1, 8, 9, 32, 33, 40, 41, 48, 49, 57, 65, 121} {
				configs = append(configs, config{size, artifacts, materials})
			}
		}
	}
	configs = append(configs, config{[2]int{1280, 720}, 1800, 3}, config{[2]int{1920, 1080}, 1801, 2})

	for _, c := range configs {
		sim := &simulatedInventory{
			width: c.size[0], height: c.size[1],
			grid:      layout.Default().Inventory.Grid,
			panel:     layout.Default().Inventory.Panel,
			artifacts: c.artifacts,
			materials: c.materials,
		}
		seen := make(map[int]int)
		err := sim.walker().Walk(t.Context(), func(f Frame) error {
			seen[sim.itemIn(f.Image)]++
			if f.Duplicate {
				t.Errorf("%+v: frame %d flagged duplicate", c, f.Index)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if len(seen) != c.artifacts {
			t.Errorf("%+v: saw %d distinct items", c, len(seen))
		}
		for item, times := range seen {
			if item >= c.artifacts {
				t.Errorf("%+v: captured non-artifact %d", c, item)
			}
			if times != 1 {
				t.Errorf("%+v: item %d captured %d times", c, item, times)
			}
		}
	}
}

// The real inventory that exposed this had 384 artifacts and 364 materials, with the
// on-screen count of 748 including both.
func TestInventoryWalkEndsAtMaterialsCountedInTotal(t *testing.T) {
	for _, c := range []struct{ artifacts, materials int }{{384, 364}, {40, 32}, {8, 40}, {100, 100}} {
		sim := &simulatedInventory{
			width: 1280, height: 720,
			grid:      layout.Default().Inventory.Grid,
			panel:     layout.Default().Inventory.Panel,
			artifacts: c.artifacts,
			materials: c.materials,
		}
		w := sim.walker()
		w.Count = c.artifacts + c.materials
		seen := make(map[int]bool)
		err := w.Walk(t.Context(), func(f Frame) error {
			seen[sim.itemIn(f.Image)] = true
			return nil
		})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		for item := range c.artifacts {
			if !seen[item] {
				t.Errorf("%+v: artifact %d was missed", c, item)
			}
		}
	}
}

// The real inventory that exposed this showed 481 artifacts and 7 materials, while its
// count said 577. The walk kept clicking the bottom screen once the list stopped
// scrolling, and captured the last rows four times.
func TestInventoryWalkStopsWhereTheListEndsWhenTheCountIsTooHigh(t *testing.T) {
	type config struct {
		size                        [2]int
		artifacts, materials, count int
	}
	configs := []config{
		{[2]int{1920, 1080}, 481, 7, 577},
		{[2]int{1280, 720}, 481, 7, 577},
		{[2]int{1280, 720}, 100, 0, 140},
		{[2]int{1280, 720}, 100, 4, 120},
		{[2]int{1280, 720}, 100, 0, 108},
		{[2]int{1280, 720}, 96, 0, 104},
		{[2]int{1280, 800}, 57, 3, 300},
		{[2]int{1280, 720}, 20, 0, 200},
	}
	for _, c := range configs {
		sim := &simulatedInventory{
			width: c.size[0], height: c.size[1],
			grid:      layout.Default().Inventory.Grid,
			panel:     layout.Default().Inventory.Panel,
			artifacts: c.artifacts,
			materials: c.materials,
		}
		w := sim.walker()
		w.Count = c.count
		seen := make(map[int]int)
		err := w.Walk(t.Context(), func(f Frame) error {
			seen[sim.itemIn(f.Image)]++
			if f.Duplicate {
				t.Errorf("%+v: frame %d flagged duplicate", c, f.Index)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		for item := range c.artifacts + c.materials {
			if seen[item] != 1 {
				t.Errorf("%+v: item %d captured %d times, want once", c, item, seen[item])
			}
		}
		if len(seen) != c.artifacts+c.materials {
			t.Errorf("%+v: saw %d distinct items, want %d", c, len(seen), c.artifacts+c.materials)
		}
	}
}

// Weapons come in stacks of identical copies, so whole rows can repeat the row before
// them. That must not be taken for a list that stopped scrolling. Two cases can't be
// told apart. When a list ends before its count, copies of one item in its last row
// look the same as empty cells, and are dropped like them. And a list that is short
// and identical everywhere but its unvisited end can't be measured, so it is walked
// as far as its count says.
func TestInventoryWalkKeepsGoingThroughStacksOfIdenticalItems(t *testing.T) {
	for _, c := range []struct {
		items, count int
		stack        [2]int
	}{
		{200, 200, [2]int{40, 100}},
		{200, 200, [2]int{120, 200}},
		{200, 200, [2]int{0, 200}},
		{150, 230, [2]int{20, 144}},
		{200, 200, [2]int{127, 200}},
		{200, 200, [2]int{96, 200}},
		{200, 200, [2]int{1, 200}},
		{300, 300, [2]int{8, 299}},
		{150, 230, [2]int{100, 144}},
		{481, 577, [2]int{440, 480}},
	} {
		sim := &simulatedInventory{
			width: 1280, height: 720,
			grid:      layout.Default().Inventory.Grid,
			panel:     layout.Default().Inventory.Panel,
			artifacts: c.items,
			stack:     c.stack,
		}
		w := sim.walker()
		w.Count = c.count
		seen := make(map[int]int)
		frames := 0
		err := w.Walk(t.Context(), func(f Frame) error {
			frames++
			seen[sim.itemIn(f.Image)]++
			return nil
		})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if frames != c.items {
			t.Errorf("%+v: %d frames, want one per item", c, frames)
		}
		for item := range c.items {
			want := 1
			if item == c.stack[0] {
				want = c.stack[1] - c.stack[0]
			} else if item > c.stack[0] && item < c.stack[1] {
				want = 0
			}
			if seen[item] != want {
				t.Errorf("%+v: item %d seen %d times, want %d", c, item, seen[item], want)
				break
			}
		}
	}
}

// A count too high by less than a row, with the same number of rows, leaves empty
// cells at the end of the last row. Clicking one leaves the panel as it was, which
// can't be told from another copy of the last item, so the extra frames are flagged
// as duplicates rather than dropped.
func TestInventoryWalkFlagsEmptyCellsInTheLastRow(t *testing.T) {
	sim := &simulatedInventory{width: 1280, height: 720, grid: layout.Default().Inventory.Grid, panel: layout.Default().Inventory.Panel, artifacts: 100}
	w := sim.walker()
	w.Count = 102
	seen := make(map[int]int)
	var duplicates []int
	err := w.Walk(t.Context(), func(f Frame) error {
		seen[sim.itemIn(f.Image)]++
		if f.Duplicate {
			duplicates = append(duplicates, f.Index)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 100 || !slices.Equal(duplicates, []int{100, 101}) {
		t.Errorf("saw %d items, duplicates at %v; want 100 items and duplicates at [100 101]", len(seen), duplicates)
	}
}

func TestInventoryWalkHonorsLimit(t *testing.T) {
	sim := &simulatedInventory{width: 1280, height: 720, grid: layout.Default().Inventory.Grid, panel: layout.Default().Inventory.Panel, artifacts: 500}
	w := sim.walker()
	w.Limit = 10
	frames := 0
	if err := w.Walk(t.Context(), func(Frame) error { frames++; return nil }); err != nil {
		t.Fatal(err)
	}
	if frames != 10 {
		t.Errorf("frames = %d, want 10", frames)
	}
}

func TestInventoryWalkFlagsStuckClicks(t *testing.T) {
	sim := &simulatedInventory{
		width: 1280, height: 720, grid: layout.Default().Inventory.Grid, panel: layout.Default().Inventory.Panel,
		artifacts: 3, stuckClicks: true,
	}
	var flags []bool
	if err := sim.walker().Walk(t.Context(), func(f Frame) error { flags = append(flags, f.Duplicate); return nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(flags, []bool{false, true, true}) {
		t.Errorf("duplicate flags = %v", flags)
	}
}
