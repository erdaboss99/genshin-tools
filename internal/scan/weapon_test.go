package scan

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"testing"

	"github.com/erdaboss99/gensin-tools/internal/catalog"
	"github.com/erdaboss99/gensin-tools/internal/game"
	"github.com/erdaboss99/gensin-tools/internal/layout"
)

func weaponParser(replies ...string) WeaponParser {
	cat := catalog.New(catalog.Data{
		Weapons:    []string{"Mistsplitter Reforged", "Amos' Bow", "Dull Blade"},
		Characters: []string{"Kamisato Ayaka", "Hu Tao"},
	})
	return WeaponParser{Layout: layout.Default().Weapons, OCR: &scriptedOCR{replies: replies}, Catalog: cat}
}

// weaponFrame is a blank frame whose name banner has the given colour.
func weaponFrame(t *testing.T, banner color.RGBA) *image.RGBA {
	t.Helper()
	win, err := layout.NewWindow(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	frame := blankFrame()
	draw.Draw(frame, win.Rect(layout.Default().Weapons.Nameplate), image.NewUniform(banner), image.Point{}, draw.Src)
	return frame
}

func TestWeaponParserReadsAWeapon(t *testing.T) {
	p := weaponParser("Mistsplitter Reforged", "Lv. 90/90", "1", "Equipped: Kamisato Ayaka")
	record, skipped, err := p.Parse(t.Context(), weaponFrame(t, nameplateColors[5].color))
	if err != nil || skipped {
		t.Fatalf("err = %v, skipped = %v", err, skipped)
	}
	want := game.Weapon{Key: "MistsplitterReforged", Level: 90, Ascension: 6, Refinement: 1, Location: "KamisatoAyaka", Lock: true}
	if record.Weapon != want {
		t.Errorf("Weapon = %+v, want %+v", record.Weapon, want)
	}
	if record.Rarity != 5 || len(record.Issues) != 0 {
		t.Errorf("Rarity = %d, Issues = %v", record.Rarity, record.Issues)
	}
}

func TestWeaponParserReportsUnreadableFields(t *testing.T) {
	p := weaponParser("Totally Unknown Sword", "??", "9", "")
	record, _, err := p.Parse(t.Context(), weaponFrame(t, nameplateColors[3].color))
	if err != nil {
		t.Fatal(err)
	}
	if record.Weapon.Validate() == nil {
		t.Error("an unreadable weapon must not validate")
	}
	for _, want := range []string{`unrecognized weapon name "Totally Unknown Sword"`, `unreadable level "??"`, `unreadable refinement "9"`} {
		if !slices.Contains(record.Issues, want) {
			t.Errorf("missing issue %q in %v", want, record.Issues)
		}
	}
}

func TestWeaponParserRetriesRefinement(t *testing.T) {
	// The first read is empty, the single-character retry finds the digit.
	p := weaponParser("Amos' Bow", "Lv. 40/50", "", "4", "")
	record, _, err := p.Parse(t.Context(), weaponFrame(t, nameplateColors[5].color))
	if err != nil {
		t.Fatal(err)
	}
	if record.Weapon.Refinement != 4 || len(record.Issues) != 0 {
		t.Errorf("Refinement = %d, Issues = %v", record.Weapon.Refinement, record.Issues)
	}
	// Every attempt failing still leaves the first read in the raw text.
	p = weaponParser("Amos' Bow", "Lv. 40/50", "x", "", "", "")
	record, _, _ = p.Parse(t.Context(), weaponFrame(t, nameplateColors[5].color))
	if record.Weapon.Refinement != 0 || record.Raw.Refinement != "x" || !slices.Contains(record.Issues, `unreadable refinement "x"`) {
		t.Errorf("Refinement = %d, Raw = %q, Issues = %v", record.Weapon.Refinement, record.Raw.Refinement, record.Issues)
	}
}

func TestWeaponParserSkipsEnhancementOre(t *testing.T) {
	for _, name := range []string{"Enhancement Ore", "Fine Enhancement Ore", "Mystic Enhancement Ore"} {
		_, skipped, err := weaponParser(name).Parse(t.Context(), blankFrame())
		if err != nil || !skipped {
			t.Errorf("%q: skipped = %v, err = %v", name, skipped, err)
		}
	}
	if _, skipped, _ := weaponParser("Dull Blade", "Lv. 1/20", "1", "").Parse(t.Context(), blankFrame()); skipped {
		t.Error("a weapon was skipped")
	}
}

func TestParseWeaponLevel(t *testing.T) {
	tests := []struct {
		text      string
		level     game.Level
		ascension int
		ok        bool
	}{
		{"Lv. 1/20", 1, 0, true},
		{"Lv. 20/20", 20, 0, true},
		{"Lv. 20/40", 20, 1, true},
		{"Lv. 90/90", 90, 6, true},
		{"Lv.4O/50", 40, 2, true},
		{"Lv. 80/91", 80, 6, true},
		{"Lv. 95/90", 0, 0, false},
		{"Lv. 10/100", 0, 0, false},
		{"Lv. 0/20", 0, 0, false},
		{"garbage", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tt := range tests {
		level, ascension, ok := parseWeaponLevel(tt.text)
		if level != tt.level || ascension != tt.ascension || ok != tt.ok {
			t.Errorf("parseWeaponLevel(%q) = %d, %d, %v; want %d, %d, %v", tt.text, level, ascension, ok, tt.level, tt.ascension, tt.ok)
		}
	}
}

func TestWeaponRarity(t *testing.T) {
	for _, c := range nameplateColors {
		if got := weaponRarity(c.color); got != c.rarity {
			t.Errorf("weaponRarity(%v) = %d, want %d", c.color, got, c.rarity)
		}
	}
	if got := weaponRarity(color.RGBA{185, 110, 55, 255}); got != 5 {
		t.Errorf("a near 5-star colour gave %d", got)
	}
}

func TestScanWeaponsNotesRepeatsWithoutIssues(t *testing.T) {
	frame := weaponFrame(t, nameplateColors[3].color)
	source := func(ctx context.Context, emit EmitFrame) error {
		if err := emit(Frame{Index: 0, Source: "a", Image: frame}); err != nil {
			return err
		}
		return emit(Frame{Index: 1, Source: "b", Image: frame, Duplicate: true})
	}
	replies := []string{"Dull Blade", "Lv. 1/20", "1", "", "Dull Blade", "Lv. 1/20", "1", ""}
	records, err := ScanWeapons(t.Context(), source, weaponParser(replies...), ScanOptions{Workers: 1})
	if err != nil || len(records) != 2 {
		t.Fatalf("records = %d, err = %v", len(records), err)
	}
	if len(records[0].Notes) != 0 || !slices.Equal(records[1].Notes, []string{RepeatNote}) {
		t.Errorf("notes = %v and %v", records[0].Notes, records[1].Notes)
	}
	for _, r := range records {
		if len(r.Issues) != 0 {
			t.Errorf("record %d has issues %v", r.Index, r.Issues)
		}
	}
}
