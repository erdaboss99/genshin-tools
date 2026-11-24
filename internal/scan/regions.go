package scan

import (
	"fmt"
	"image"

	"github.com/erdaboss99/gensin-tools/internal/imaging"
	"github.com/erdaboss99/gensin-tools/internal/layout"
)

// NamedRect is a labelled screen region, used for calibration output.
type NamedRect struct {
	Name string
	Rect image.Rectangle
}

// InventoryRegions lists the inventory regions for this frame, including the shift
// caused by a Sanctifying Elixir band.
func InventoryRegions(l layout.Inventory, win layout.Window, frame *image.RGBA) []NamedRect {
	shift := 0
	if isElixirBand(imaging.MeanColor(frame, win.Rect(l.ElixirBand))) {
		shift = l.ElixirShift
	}
	return []NamedRect{
		{"itemCount", win.Rect(l.ItemCount)},
		{"name", win.Rect(l.Name)},
		{"slot", win.Rect(l.Slot)},
		{"mainStat", win.Rect(l.MainStat)},
		{"rarity", win.Rect(l.Rarity)},
		{"level", win.Rect(l.Level.Shifted(shift))},
		{"substats", win.Rect(l.Substats.Shifted(shift))},
		{"equipped", win.Rect(l.Equipped)},
		{"lockIcon", win.Rect(l.LockIcon.Shifted(shift))},
		{"astralIcon", win.Rect(l.AstralIcon.Shifted(shift))},
	}
}

// WeaponRegions lists the weapon regions for this frame.
func WeaponRegions(l layout.Weapons, win layout.Window, _ *image.RGBA) []NamedRect {
	return []NamedRect{
		{"itemCount", win.Rect(l.ItemCount)},
		{"name", win.Rect(l.Name)},
		{"nameplate", win.Rect(l.Nameplate)},
		{"level", win.Rect(l.Level)},
		{"refinement", win.Rect(l.Refinement)},
		{"equipped", win.Rect(l.Equipped)},
		{"lockIcon", win.Rect(l.LockIcon)},
	}
}

// CharacterRegions lists the character screen regions that are read. They sit on
// different pages, so no single screenshot shows all of them in use.
func CharacterRegions(l layout.Characters, win layout.Window) []NamedRect {
	regions := []NamedRect{
		{"header", win.Rect(l.Header)},
		{"level", win.Rect(l.Level)},
		{"activate", win.Rect(l.Activate)},
	}
	// Three talents, and the fourth position that characters with an alternate sprint
	// have their burst in.
	for i, name := range []string{"auto", "skill", "burst", "burst*"} {
		regions = append(regions, NamedRect{"talent " + name, win.Rect(talentLevelBox(l, i))})
	}
	return regions
}

// talentLevelBox returns where the level of the talent at a position is shown.
func talentLevelBox(l layout.Characters, position int) layout.Box {
	return l.TalentLevel.Shifted(l.TalentStep * position)
}

// CharacterTargets lists where the character scan clicks.
func CharacterTargets(l layout.Characters, win layout.Window) []image.Point {
	points := []image.Point{
		win.Point(l.NextCharacter), win.Point(l.AttributesTab), win.Point(l.ConstellationTab), win.Point(l.TalentsTab),
	}
	for i := range 6 {
		points = append(points, win.Point(l.Constellations.At(i)))
	}
	return points
}

// AchievementRegions lists the regions of every result row found in this frame.
func AchievementRegions(l layout.Achievements, win layout.Window, frame *image.RGBA) []NamedRect {
	var regions []NamedRect
	for row, center := range findRows(l, win, frame) {
		regions = append(regions,
			NamedRect{fmt.Sprintf("title[%d]", row), win.RowRect(l.Title, center)},
			NamedRect{fmt.Sprintf("stars[%d]", row), win.RowRect(l.Stars, center)},
			NamedRect{fmt.Sprintf("status[%d]", row), win.RowRect(l.Status, center)},
		)
	}
	if len(regions) == 0 {
		regions = append(regions, NamedRect{"noResults", win.Rect(l.NoResults)})
	}
	return append(regions, NamedRect{"completed", win.Rect(l.CompletedCount)})
}
