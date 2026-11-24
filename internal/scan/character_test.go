package scan

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/erdaboss99/gensin-tools/internal/catalog"
	"github.com/erdaboss99/gensin-tools/internal/game"
	"github.com/erdaboss99/gensin-tools/internal/layout"
)

func characterCatalog() *catalog.Catalog {
	return catalog.New(catalog.Data{
		Characters: []string{"Hu Tao", "Mona", "Kamisato Ayaka", "Tartaglia", "Traveler", "Neuvillette"},
		Boosts: map[string]catalog.ConstellationBoosts{
			"Hu Tao":      {C3: game.TalentSkill, C5: game.TalentBurst},
			"Mona":        {C3: game.TalentBurst, C5: game.TalentSkill},
			"Neuvillette": {C3: game.TalentAuto, C5: game.TalentBurst},
		},
	})
}

func TestParseCharacterHeader(t *testing.T) {
	cat := characterCatalog()
	tests := []struct {
		text     string
		traveler string
		key      game.CharacterKey
		element  string
		ok       bool
	}{
		{"Pyro / Hu Tao", "", "HuTao", "Pyro", true},
		{"pyro/hu tao", "", "HuTao", "Pyro", true},
		{"Hydro\n/ Mona", "", "Mona", "Hydro", true},
		{"Cryo Kamisato\n/ Ayaka", "", "KamisatoAyaka", "Cryo", true},
		{"Anemo / Traveler", "", "TravelerAnemo", "Anemo", true},
		{"Geo / Lumine", "Lumine", "TravelerGeo", "Geo", true},
		{"Pyr0 / Hu Tao", "", "HuTao", "Pyro", true},
		{"Pyro Hu Tao", "", "", "", false},
		{"Pyro / Nobody Here", "", "", "", false},
		{"Nothing / Hu Tao", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, tt := range tests {
		h, ok := parseCharacterHeader(tt.text, cat, tt.traveler)
		if ok != tt.ok || h.Key != tt.key || h.Element != tt.element {
			t.Errorf("parseCharacterHeader(%q) = %+v, %v; want %q, %q, %v", tt.text, h, ok, tt.key, tt.element, tt.ok)
		}
	}
}

func TestParseTalentLevel(t *testing.T) {
	tests := []struct {
		text  string
		level int
		ok    bool
	}{
		{"Lv. 10", 10, true},
		{"Secret Spear of Wangsheng\nLv. 13", 13, true},
		{"Lv.1", 1, true},
		{"Lv. 15\n", 15, true},
		{"Lv. 16", 0, false},
		{"Lv. 0", 0, false},
		{"Lv.", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		level, ok := parseTalentLevel(tt.text)
		if level != tt.level || ok != tt.ok {
			t.Errorf("parseTalentLevel(%q) = %d, %v; want %d, %v", tt.text, level, ok, tt.level, tt.ok)
		}
	}
}

func TestConstellationLocked(t *testing.T) {
	if !constellationLocked(color.RGBA{230, 230, 225, 255}) {
		t.Error("a white button means the constellation is locked")
	}
	if constellationLocked(color.RGBA{60, 70, 90, 255}) || constellationLocked(color.RGBA{250, 250, 150, 255}) {
		t.Error("a dark or coloured area means the constellation is unlocked")
	}
}

func TestApplyTartagliaBuff(t *testing.T) {
	record := func(key game.CharacterKey, ascension, auto int) CharacterRecord {
		return CharacterRecord{Character: game.Character{Key: key, Ascension: ascension, Talent: game.Talents{Auto: auto, Skill: 5, Burst: 5}}}
	}
	autos := func(records []CharacterRecord) []int {
		var out []int
		for _, r := range records {
			out = append(out, r.Character.Talent.Auto)
		}
		return out
	}
	equal := func(a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	inParty := []CharacterRecord{record("HuTao", 6, 9), record("Tartaglia", 6, 9), record("Mona", 6, 9), record("Zhongli", 6, 9), record("Diluc", 6, 9)}
	if !applyTartagliaBuff(inParty) || !equal(autos(inParty), []int{8, 8, 8, 8, 9}) {
		t.Errorf("in party: %v", autos(inParty))
	}
	outside := []CharacterRecord{record("HuTao", 6, 9), record("Mona", 6, 9), record("Zhongli", 6, 9), record("Diluc", 6, 9), record("Tartaglia", 6, 9)}
	if !applyTartagliaBuff(outside) || !equal(autos(outside), []int{9, 9, 9, 9, 8}) {
		t.Errorf("outside party: %v", autos(outside))
	}
	low := []CharacterRecord{record("Tartaglia", 3, 9), record("Mona", 6, 9)}
	if applyTartagliaBuff(low) || !equal(autos(low), []int{9, 9}) {
		t.Errorf("below ascension 4: %v", autos(low))
	}
	if applyTartagliaBuff([]CharacterRecord{record("HuTao", 6, 9)}) {
		t.Error("no Tartaglia, nothing to apply")
	}
}

// characterGame simulates the character screen. Which constellations are unlocked
// decides whether the "Activate" button is drawn white after a click on a node.
type characterGame struct {
	layout   layout.Characters
	win      layout.Window
	unlocked []int
	current  int
	lastNode int // index of the constellation node clicked last, or -1
	clicks   []image.Point
	escapes  int
	failNext bool
}

func newCharacterGame(unlocked ...int) *characterGame {
	win, _ := layout.NewWindow(1280, 720)
	return &characterGame{layout: layout.Default().Characters, win: win, unlocked: unlocked, lastNode: -1}
}

func (g *characterGame) Size() (int, int) { return 1280, 720 }

func (g *characterGame) Capture() (*image.RGBA, error) {
	frame := blankFrame()
	if g.lastNode >= 0 && g.lastNode >= g.unlocked[g.current] {
		draw.Draw(frame, g.win.Rect(g.layout.Activate), image.NewUniform(color.RGBA{240, 240, 235, 255}), image.Point{}, draw.Src)
	}
	return frame, nil
}

func (g *characterGame) Click(x, y int) error {
	at := image.Pt(x, y)
	g.clicks = append(g.clicks, at)
	g.lastNode = -1
	switch at {
	case g.win.Point(g.layout.NextCharacter):
		g.current = (g.current + 1) % len(g.unlocked)
	}
	for i := range 6 {
		if at == g.win.Point(g.layout.Constellations.At(i)) {
			g.lastNode = i
		}
	}
	return nil
}

func (g *characterGame) PressEscape() error {
	g.escapes++
	g.lastNode = -1
	return nil
}

func (g *characterGame) MoveTo(int, int) error { return nil }
func (g *characterGame) Scroll(int) error      { return nil }
func (g *characterGame) Type(string) error     { return nil }
func (g *characterGame) PressEnter() error     { return nil }

func characterScanner(g *characterGame, replies ...string) CharacterScanner {
	return CharacterScanner{
		Game: g, Layout: g.layout, OCR: &scriptedOCR{replies: replies},
		Catalog: characterCatalog(), Pace: 0,
	}
}

func TestCharacterScannerWalksAllCharacters(t *testing.T) {
	g := newCharacterGame(1, 5, 0)
	s := characterScanner(g,
		"Pyro / Hu Tao", "Lv. 90/90", "Lv. 10", "Lv. 10", "Lv. 10",
		"Hydro / Mona", "Lv. 80/90", "Lv. 8", "Lv. 9", "Lv. 9",
		"Anemo / Traveler", "Lv. 20/40", "Lv. 1", "Lv. 1", "Lv. 1",
		"Pyro / Hu Tao",
	)
	records, err := s.Scan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []game.Character{
		{Key: "HuTao", Level: 90, Ascension: 6, Constellation: 1, Talent: game.Talents{Auto: 10, Skill: 10, Burst: 10}},
		// Mona's constellation 3 raises the burst and 5 the skill, so both lose 3 levels.
		{Key: "Mona", Level: 80, Ascension: 6, Constellation: 5, Talent: game.Talents{Auto: 8, Skill: 6, Burst: 6}},
		{Key: "TravelerAnemo", Level: 20, Ascension: 1, Constellation: 0, Talent: game.Talents{Auto: 1, Skill: 1, Burst: 1}},
	}
	if len(records) != len(want) {
		t.Fatalf("got %d records, want %d: %+v", len(records), len(want), records)
	}
	for i, w := range want {
		if records[i].Character != w {
			t.Errorf("character %d = %+v, want %+v", i, records[i].Character, w)
		}
		if len(records[i].Issues) != 0 {
			t.Errorf("character %d has issues %v", i, records[i].Issues)
		}
		if records[i].Index != i {
			t.Errorf("character %d has index %d", i, records[i].Index)
		}
	}
	if g.escapes != 6 {
		t.Errorf("Escape pressed %d times, want 2 per character", g.escapes)
	}
	if first := g.clicks[0]; first != g.win.Point(g.layout.AttributesTab) {
		t.Errorf("first click at %v, want the attributes tab", first)
	}
}

func TestCharacterScannerSkipsAltSprint(t *testing.T) {
	g := newCharacterGame(0)
	s := characterScanner(g,
		"Hydro / Mona", "Lv. 1/20", "Lv. 1", "Lv. 1", "Lv. 1",
		"Hydro / Mona",
	)
	if _, err := s.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	burst := g.win.Point(g.layout.Talents.At(3))
	skillOnly := g.win.Point(g.layout.Talents.At(2))
	var sawBurst, sawPlain bool
	for _, c := range g.clicks {
		sawBurst = sawBurst || c == burst
		sawPlain = sawPlain || c == skillOnly
	}
	if !sawBurst || sawPlain {
		t.Errorf("Mona's burst should be the fourth talent: fourth clicked = %v, third clicked = %v", sawBurst, sawPlain)
	}
}

func TestCharacterScannerFlagsMissingBoostData(t *testing.T) {
	g := newCharacterGame(3)
	s := characterScanner(g,
		"Anemo / Traveler", "Lv. 90/90", "Lv. 10", "Lv. 13", "Lv. 13",
		"Anemo / Traveler",
	)
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err = %v", len(records), err)
	}
	if len(records[0].Issues) != 1 || records[0].Character.Talent.Skill != 13 {
		t.Errorf("issues = %v, talents = %+v", records[0].Issues, records[0].Character.Talent)
	}
}

func TestCharacterScannerGivesUpWithoutNames(t *testing.T) {
	g := newCharacterGame(0, 0, 0, 0, 0)
	records, err := characterScanner(g).Scan(t.Context())
	// The record that hits the limit is not kept, as the scan stops there.
	if err == nil || len(records) != unreadableLimit-1 {
		t.Errorf("records = %d, err = %v; want %d records and an error", len(records), err, unreadableLimit-1)
	}
}

func TestCharacterScannerLimit(t *testing.T) {
	g := newCharacterGame(0, 0, 0)
	s := characterScanner(g,
		"Pyro / Hu Tao", "Lv. 1/20", "Lv. 1", "Lv. 1", "Lv. 1",
		"Hydro / Mona", "Lv. 1/20", "Lv. 1", "Lv. 1", "Lv. 1",
	)
	s.Limit = 2
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 2 {
		t.Errorf("records = %d, err = %v", len(records), err)
	}
}
