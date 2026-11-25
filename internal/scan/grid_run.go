package scan

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"time"

	"github.com/erdaboss99/genshin-tools/internal/imaging"
	"github.com/erdaboss99/genshin-tools/internal/layout"
)

// gridRun is the state of one traversal.
type gridRun struct {
	walker   InventoryWalker
	ctx      context.Context
	win      layout.Window
	emit     EmitFrame
	total    int
	captured int
	scrolled int
	lastHash uint64
	// recent holds the panel fingerprints of the latest rows visited, a screen's
	// worth, to notice when the game stops scrolling before the count says it should.
	recent []visitedRow
	// lastRow is the latest list row visited.
	lastRow int
	// rows is how many rows the item count gives the list.
	rows int
	// realRows is how many rows the list turned out to have, once it is known to be
	// shorter than its count.
	realRows int
	// seen places each item by its panel fingerprint, for working out where the
	// list is scrolled to.
	seen map[uint64]placement
	// pending holds duplicate frames until a different item follows, because a run
	// of them at the end of a list shorter than its count may be empty cells.
	pending []shot
}

// placement is where an item was seen. A fingerprint seen more than once, such as a
// stack of identical items, places nothing, since more copies may sit further down.
type placement struct {
	row, col int
	times    int
}

// visitedRow is one row of the list and the fingerprints of the items visited in it.
type visitedRow struct {
	row    int
	hashes []uint64
}

// shot is a captured item that has not been emitted yet.
type shot struct {
	frame     *image.RGBA
	hash      uint64
	duplicate bool
	// row is the list row the item was clicked in.
	row int
}

var (
	errWalkDone = errors.New("all items captured")
	// errListEnded means the list ended before its item count was reached.
	errListEnded = errors.New("list ended before the item count")
)

const (
	// settleAttempts limits how many times captureSettled looks again.
	settleAttempts = 6

	cursorSettleDelay = 80 * time.Millisecond
	bottomScrollDelay = 200 * time.Millisecond
	scrollDelay       = 100 * time.Millisecond
	clickDelay        = 100 * time.Millisecond
	duplicateDelay    = 250 * time.Millisecond
	settlePollDelay   = 40 * time.Millisecond

	// bottomScrollExtraRows overshoots the list's end, which the game clamps.
	bottomScrollExtraRows = 2

	liveSourceFormat = "live:%d"
)

// walk visits items page by page. The grid only scrolls in whole-row steps while
// the list is long enough, and the last rows are only reachable near the bottom,
// so the traversal has three phases: full pages, a partial page to reach the
// second-to-last row, and the bottom row anchored to the end of the list.
func (g *gridRun) walk() error {
	if g.total <= 0 {
		return nil
	}
	g.lastRow = -1
	g.seen = make(map[uint64]placement)
	grid := g.walker.Layout.Grid
	rowsPerPage := grid.RowsPerPage(g.win)
	rows := (g.total-1)/grid.Columns + 1
	g.rows = rows
	pages := max(1, (rows-1)/rowsPerPage)

	if err := g.walker.Game.MoveTo(g.cell(0, 0).X, g.cell(0, 0).Y); err != nil {
		return err
	}
	if err := g.wait(cursorSettleDelay); err != nil {
		return err
	}

	err := g.visitPages(pages, rowsPerPage)
	if err == nil {
		lastPageRow := (pages - 1) * rowsPerPage
		err = g.visitPartialPage(rows, rowsPerPage, lastPageRow)
	}
	if err == nil {
		err = g.visitBottom()
	}
	switch {
	case errors.Is(err, errListEnded):
		if err := g.dropEmptyCells(); err != nil {
			return err
		}
		g.warnShortList()
		return nil
	case err == nil || errors.Is(err, errWalkDone):
		return g.flush()
	}
	return err
}

func (g *gridRun) visitPages(pages, rowsPerPage int) error {
	for page := range pages {
		top := page * rowsPerPage
		if err := g.scrollToRow(top); err != nil {
			return err
		}
		for row := range rowsPerPage {
			var err error
			if page > 0 && row == 0 {
				// The first row after a scroll is checked, since a list shorter than
				// the count stops scrolling early and shows rows already visited.
				err = g.visitCheckedRow(top, 0, top)
			} else {
				err = g.visitRow(top+row, g.rowCells(row))
			}
			if errors.Is(err, errListShort) {
				return g.finishShortList()
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *gridRun) visitPartialPage(rows, rowsPerPage, currentRow int) error {
	target := rows - rowsPerPage - 1
	if target <= currentRow {
		return nil
	}
	if err := g.scrollToRow(target); err != nil {
		return err
	}
	first := currentRow + rowsPerPage - target
	for row := first; row < rowsPerPage; row++ {
		var err error
		if row == first {
			err = g.visitCheckedRow(target+row, row, target)
		} else {
			err = g.visitRow(target+row, g.rowCells(row))
		}
		if errors.Is(err, errListShort) {
			return g.finishShortList()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *gridRun) visitBottom() error {
	grid := g.walker.Layout.Grid
	if err := g.scrollToBottom(); err != nil {
		return err
	}
	materialRows, found, err := g.countMaterialRows()
	if err != nil {
		return err
	}
	if !found {
		// The item count can include upgrade materials, and these sort after the
		// artifacts, so nothing is left to read.
		if g.walker.Log != nil {
			g.walker.Log.Info("reached the upgrade materials at the end of the inventory", "skipped", g.total-g.taken())
		}
		return nil
	}
	cells := make([]image.Point, 0, grid.Columns)
	for col := range grid.Columns {
		cells = append(cells, g.bottomCell(materialRows, col))
	}
	row := g.lastRow + 1
	shots, err := g.captureRow(row, cells)
	if err != nil {
		return err
	}
	if len(g.recent) > 0 && startsWith(g.recent[len(g.recent)-1].hashes, hashesOf(shots)) {
		// The bottom row looks like the last one visited: either it is that row,
		// because the screen was at the end of the list already and the count is
		// higher than the list, or both rows hold identical items.
		rows, placed, err := g.measureRows()
		if err != nil {
			return err
		}
		if placed && rows <= row {
			g.realRows = rows
			return errListEnded
		}
	}
	return g.emitRow(row, shots)
}

// errListShort means the game stopped scrolling before the end of the list as the
// count gives it, because the list is shorter. Its length is in gridRun.realRows.
var errListShort = errors.New("list shorter than its count")

// rowCells returns the cells of a screen row counted from the top.
func (g *gridRun) rowCells(row int) []image.Point {
	cells := make([]image.Point, g.walker.Layout.Grid.Columns)
	for col := range cells {
		cells[col] = g.cell(row, col)
	}
	return cells
}

// visitRow visits the cells of list row row, up to the item count, emitting each
// as it goes.
func (g *gridRun) visitRow(row int, cells []image.Point) error {
	cells = cells[:min(len(cells), g.total-g.taken())]
	hashes := make([]uint64, 0, len(cells))
	for _, at := range cells {
		s, err := g.shoot(at, row)
		if err != nil {
			return err
		}
		hashes = append(hashes, s.hash)
		if err := g.emitShot(s); err != nil {
			return err
		}
	}
	g.remember(row, hashes)
	return nil
}

// visitCheckedRow visits list row row, shown on screen row screenRow after a scroll
// to list row top, holding it back until it is known to be new. A row that repeats
// one visited just before means the game stopped scrolling early, unless the rows
// hold identical items, so the list is measured. If it is shorter than its count,
// nothing is emitted, and it returns errListShort.
func (g *gridRun) visitCheckedRow(row, screenRow, top int) error {
	shots, err := g.captureRow(row, g.rowCells(screenRow))
	if err != nil {
		return err
	}
	if g.repeatsRecentRow(hashesOf(shots)) {
		rows, placed, err := g.measureRows()
		if err != nil {
			return err
		}
		// The probe's clicks aren't items of the walk, so the held row's last item
		// stays the one later clicks are compared with.
		g.lastHash = shots[len(shots)-1].hash
		if placed && rows < g.rows {
			g.realRows = rows
			return errListShort
		}
		if err := g.scrollBackTo(top); err != nil {
			return err
		}
	}
	return g.emitRow(row, shots)
}

// captureRow clicks the cells of list row row, up to the item count, without
// emitting them.
func (g *gridRun) captureRow(row int, cells []image.Point) ([]shot, error) {
	cells = cells[:min(len(cells), g.total-g.taken())]
	shots := make([]shot, 0, len(cells))
	for _, at := range cells {
		s, err := g.shoot(at, row)
		if err != nil {
			return nil, err
		}
		shots = append(shots, s)
	}
	return shots, nil
}

// emitRow records and emits a row captured by captureRow.
func (g *gridRun) emitRow(row int, shots []shot) error {
	g.remember(row, hashesOf(shots))
	for _, s := range shots {
		if err := g.emitShot(s); err != nil {
			return err
		}
	}
	return nil
}

func hashesOf(shots []shot) []uint64 {
	hashes := make([]uint64, len(shots))
	for i, s := range shots {
		hashes[i] = s.hash
	}
	return hashes
}

// startsWith reports whether row begins with the non-empty prefix.
func startsWith(row, prefix []uint64) bool {
	return len(prefix) > 0 && len(row) >= len(prefix) && slices.Equal(row[:len(prefix)], prefix)
}

// repeatsRecentRow reports whether a recently visited row starts with hashes.
func (g *gridRun) repeatsRecentRow(hashes []uint64) bool {
	for _, r := range g.recent {
		if startsWith(r.hashes, hashes) {
			return true
		}
	}
	return false
}

// measureRows finds how many rows the list really has. It scrolls to the bottom,
// where the list ends a screen below the top screen row, and clicks that row's first
// cell. If the item there was seen exactly once, in the first column, its row places
// the screen. Otherwise, inside a stack of identical items or below the rows visited,
// it scrolls up a row at a time until it reaches an item it can place. Only item panels
// are compared, because the grid itself animates. placed is false when nothing on
// the way up can be placed, such as when every item is identical.
func (g *gridRun) measureRows() (rows int, placed bool, err error) {
	if err := g.scrollToBottom(); err != nil {
		return 0, false, err
	}
	rowsPerPage := g.walker.Layout.Grid.RowsPerPage(g.win)
	perRow := g.walker.Layout.Grid.ScrollTicksPerRow
	for up := 0; up < g.rows; up++ {
		if up > 0 {
			// Rows take a fractional number of ticks, so each step is the change in
			// the rounded total, which keeps the rounding from adding up.
			ticks := int(math.Round(float64(up)*perRow)) - int(math.Round(float64(up-1)*perRow))
			if err := g.walker.Game.Scroll(ticks); err != nil {
				return 0, false, err
			}
			g.scrolled -= ticks
			if err := g.wait(scrollDelay); err != nil {
				return 0, false, err
			}
		}
		s, err := g.shoot(g.cell(0, 0), -1)
		if err != nil {
			return 0, false, err
		}
		if p, ok := g.seen[s.hash]; ok && p.times == 1 && p.col == 0 {
			return p.row + up + rowsPerPage, true, nil
		}
	}
	return 0, false, nil
}

// scrollToBottom scrolls past the end of the list as the count gives it, which the
// game stops at the real end.
func (g *gridRun) scrollToBottom() error {
	if err := g.scrollToRow(g.total/g.walker.Layout.Grid.Columns + bottomScrollExtraRows); err != nil {
		return err
	}
	return g.wait(bottomScrollDelay)
}

// scrollBackTo returns to list row row after scrollToBottom. The game stopped at the
// end of the list rather than where the scroll asked for, so scrolling up from
// there by the difference would land short. Instead it scrolls past the top, where
// the list stops at row 0, and then down to the row.
func (g *gridRun) scrollBackTo(row int) error {
	if err := g.walker.Game.Scroll(g.scrolled + bottomScrollExtraRows*int(math.Ceil(g.walker.Layout.Grid.ScrollTicksPerRow))); err != nil {
		return err
	}
	g.scrolled = 0
	if err := g.wait(bottomScrollDelay); err != nil {
		return err
	}
	return g.scrollToRow(row)
}

// remember records a visited row, keeping a screen's worth of rows and the place of
// every item.
func (g *gridRun) remember(row int, hashes []uint64) {
	for col, h := range hashes {
		p, ok := g.seen[h]
		if !ok {
			p = placement{row: row, col: col}
		}
		p.times++
		g.seen[h] = p
	}
	g.recent = append(g.recent, visitedRow{row, hashes})
	if keep := g.walker.Layout.Grid.RowsPerPage(g.win); len(g.recent) > keep {
		g.recent = g.recent[len(g.recent)-keep:]
	}
	g.lastRow = row
}

// finishShortList visits what is left of a list shorter than its count, which has
// g.realRows rows, and returns errListEnded. Rows above the last screen are visited
// a page at a time as usual, which works now that the list's length is known. The
// last screen's rows are visited from the bottom of the list. Its last row may be
// only partly filled, and its empty cells end up as the trailing duplicates that
// walk drops.
func (g *gridRun) finishShortList() error {
	rowsPerPage := g.walker.Layout.Grid.RowsPerPage(g.win)
	lastScreen := g.realRows - rowsPerPage
	for g.lastRow+1 < lastScreen {
		top := g.lastRow + 1
		if err := g.scrollBackTo(top); err != nil {
			return err
		}
		for row := 0; row < rowsPerPage && top+row < lastScreen; row++ {
			if err := g.visitRow(top+row, g.rowCells(row)); err != nil {
				return err
			}
		}
	}
	if err := g.scrollToBottom(); err != nil {
		return err
	}
	newRows := min(max(g.realRows-1-g.lastRow, 0), rowsPerPage)
	for fromBottom := newRows - 1; fromBottom >= 0; fromBottom-- {
		row := g.realRows - 1 - fromBottom
		cells := make([]image.Point, g.walker.Layout.Grid.Columns)
		for col := range cells {
			cells[col] = g.bottomCell(fromBottom, col)
		}
		if err := g.visitRow(row, cells); err != nil {
			return err
		}
	}
	return errListEnded
}

// countMaterialRows finds how many rows of upgrade materials sit below the
// artifacts. They sort after every artifact, so the walk checks upward from the
// bottom until it sees a real artifact. found is false when the whole screen holds
// materials.
func (g *gridRun) countMaterialRows() (rows int, found bool, err error) {
	maxRows := g.walker.Layout.Grid.RowsPerPage(g.win)
	for rows := range maxRows {
		at := g.bottomCell(rows, 0)
		if err := g.click(at); err != nil {
			return 0, false, err
		}
		frame, err := g.walker.Game.Capture()
		if err != nil {
			return 0, false, err
		}
		unrelated, err := g.walker.IsMaterial(g.ctx, frame)
		if err != nil {
			return 0, false, err
		}
		if !unrelated {
			return rows, true, nil
		}
	}
	return 0, false, nil
}

func (g *gridRun) cell(row, col int) image.Point {
	grid := g.walker.Layout.Grid
	first := g.win.Point(grid.First)
	return image.Pt(first.X+g.win.Len(grid.DeltaX)*col, first.Y+g.win.Len(grid.DeltaY)*row)
}

func (g *gridRun) bottomCell(rowsFromBottom, col int) image.Point {
	grid := g.walker.Layout.Grid
	x := g.win.Point(grid.First).X + g.win.Len(grid.DeltaX)*col
	y := g.win.Y(grid.LastRowY) - g.win.Len(grid.DeltaY)*rowsFromBottom
	return image.Pt(x, y)
}

func (g *gridRun) scrollToRow(row int) error {
	ticks := int(math.Round(float64(row) * g.walker.Layout.Grid.ScrollTicksPerRow))
	if err := g.walker.Game.Scroll(g.scrolled - ticks); err != nil {
		return err
	}
	g.scrolled = ticks
	return g.wait(scrollDelay)
}

func (g *gridRun) click(at image.Point) error {
	if err := g.walker.Game.Click(at.X, at.Y); err != nil {
		return err
	}
	return g.wait(clickDelay)
}

// shoot selects the item at a cell of list row row and captures it, without
// emitting it. An unchanged panel gets one retry, in case the game was slow to redraw.
func (g *gridRun) shoot(at image.Point, row int) (shot, error) {
	if err := g.click(at); err != nil {
		return shot{}, err
	}
	panel := g.win.Rect(g.walker.Layout.Panel)
	frame, hash, err := g.captureSettled(panel)
	if err != nil {
		return shot{}, err
	}
	duplicate := g.captured > 0 && hash == g.lastHash
	if duplicate {
		if err := g.wait(duplicateDelay); err != nil {
			return shot{}, err
		}
		if frame, hash, err = g.captureSettled(panel); err != nil {
			return shot{}, err
		}
		duplicate = hash == g.lastHash
	}
	g.lastHash = hash
	return shot{frame: frame, hash: hash, duplicate: duplicate, row: row}, nil
}

// emitShot hands a captured item to the pipeline, holding duplicates back until a
// different item follows. It returns errWalkDone once the item count is reached.
func (g *gridRun) emitShot(s shot) error {
	if s.duplicate {
		g.pending = append(g.pending, s)
	} else {
		if err := g.flush(); err != nil {
			return err
		}
		if err := g.send(s); err != nil {
			return err
		}
	}
	if g.taken() >= g.total {
		if err := g.flush(); err != nil {
			return err
		}
		return errWalkDone
	}
	return nil
}

// flush emits the duplicates held back so far.
func (g *gridRun) flush() error {
	for _, s := range g.pending {
		if err := g.send(s); err != nil {
			return err
		}
	}
	g.pending = nil
	return nil
}

func (g *gridRun) send(s shot) error {
	err := g.emit(Frame{Index: g.captured, Source: fmt.Sprintf(liveSourceFormat, g.captured), Image: s.frame, Duplicate: s.duplicate})
	if err != nil {
		return err
	}
	g.captured++
	return nil
}

// dropEmptyCells settles the duplicates held back when the list ended before its
// count. Every row but the last is full, so only the last row can end in empty
// cells, and duplicates in earlier rows are real copies. A list no longer than a
// screen may have empty rows below it as well, so then all are dropped.
func (g *gridRun) dropEmptyCells() error {
	pending := g.pending
	g.pending = nil
	if g.realRows <= g.walker.Layout.Grid.RowsPerPage(g.win) {
		return nil
	}
	for _, s := range pending {
		if s.row < g.realRows-1 {
			if err := g.send(s); err != nil {
				return err
			}
		}
	}
	return nil
}

// taken counts the items captured, including duplicates held back.
func (g *gridRun) taken() int {
	return g.captured + len(g.pending)
}

// warnShortList reports that the list ended before the item count was reached.
func (g *gridRun) warnShortList() {
	if g.walker.Log != nil {
		g.walker.Log.Warn("the inventory ended before its item count; if a filter is on, clear it, or pass --count",
			"captured", g.captured, "count", g.total)
	}
}

// captureSettled waits for area to stop changing, as the panel fades between items
// for longer than a click's fixed delay. It gives up after a few tries and uses the
// latest frame, in case something there animates continuously.
func (g *gridRun) captureSettled(area image.Rectangle) (*image.RGBA, uint64, error) {
	frame, err := g.walker.Game.Capture()
	if err != nil {
		return nil, 0, err
	}
	hash := imaging.Fingerprint(frame, area)
	for range settleAttempts {
		if err := g.wait(settlePollDelay); err != nil {
			return nil, 0, err
		}
		next, err := g.walker.Game.Capture()
		if err != nil {
			return nil, 0, err
		}
		nextHash := imaging.Fingerprint(next, area)
		settled := nextHash == hash
		frame, hash = next, nextHash
		if settled {
			break
		}
	}
	return frame, hash, nil
}

func (g *gridRun) wait(d time.Duration) error {
	return g.walker.Pace.sleep(g.ctx, d)
}
