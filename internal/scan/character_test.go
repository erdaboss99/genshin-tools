package scan

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/erdaboss99/gensin-tools/internal/catalog"
	"github.com/erdaboss99/gensin-tools/internal/game"
	"github.com/erdaboss99/gensin-tools/internal/imaging"
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
// decides whether the "Activate" button is drawn white after a click on a node, and
// blue says which talent positions are drawn in blue on the talents page.
type characterGame struct {
	layout   layout.Characters
	win      layout.Window
	unlocked []int
	blue     [][]int
	current  int
	lastNode int // index of the constellation node clicked last, or -1
	clicks   []image.Point
	escapes  int
}

func newCharacterGame(unlocked ...int) *characterGame {
	win, _ := layout.NewWindow(1280, 720)
	return &characterGame{layout: layout.Default().Characters, win: win, unlocked: unlocked, blue: make([][]int, len(unlocked)), lastNode: -1}
}

func (g *characterGame) Size() (int, int) { return 1280, 720 }

func (g *characterGame) Capture() (*image.RGBA, error) {
	frame := blankFrame()
	if g.lastNode >= 0 && g.lastNode >= g.unlocked[g.current] {
		draw.Draw(frame, g.win.Rect(g.layout.Activate), image.NewUniform(color.RGBA{240, 240, 235, 255}), image.Point{}, draw.Src)
	}
	for _, position := range g.blue[g.current] {
		draw.Draw(frame, g.win.Rect(talentLevelBox(g.layout, position)), image.NewUniform(color.RGBA{80, 230, 250, 255}), image.Point{}, draw.Src)
	}
	return frame, nil
}

func (g *characterGame) Click(x, y int) error {
	at := image.Pt(x, y)
	g.clicks = append(g.clicks, at)
	g.lastNode = -1
	if at == g.win.Point(g.layout.NextCharacter) {
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
	// Mona's skill is second and her burst is fourth, behind an alternate sprint, and
	// both are blue because constellation 5 raises them.
	g.blue[1] = []int{1, 3}
	s := characterScanner(g,
		"Pyro / Hu Tao", "Level 90 / 90", "Lv. 10", "Lv. 10", "Lv. 10",
		"Hydro / Mona", "Level 80 / 90", "Lv. 8", "Lv. 9", "Lv. 9",
		"Anemo / Traveler", "Level 20 / 40", "Lv. 1", "Lv. 1", "Lv. 1",
		"Pyro / Hu Tao",
	)
	records, err := s.Scan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []game.Character{
		{Key: "HuTao", Level: 90, Ascension: 6, Constellation: 1, Talent: game.Talents{Auto: 10, Skill: 10, Burst: 10}},
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
	if !slices.Equal(records[1].Boosted, []game.TalentKind{game.TalentSkill, game.TalentBurst}) {
		t.Errorf("Mona's boosted talents = %v", records[1].Boosted)
	}
	// Only the constellation page needs closing, once per character.
	if g.escapes != 3 {
		t.Errorf("Escape pressed %d times, want once per character", g.escapes)
	}
	if first := g.clicks[0]; first != g.win.Point(g.layout.AttributesTab) {
		t.Errorf("first click at %v, want the attributes tab", first)
	}
}

func TestCharacterScannerUsesBlueWhenThereIsNoData(t *testing.T) {
	// The Traveler has no constellation data, but the blue talent says which was raised.
	g := newCharacterGame(3)
	g.blue[0] = []int{2}
	s := characterScanner(g,
		"Anemo / Traveler", "Level 90 / 90", "Lv. 10", "Lv. 10", "Lv. 13",
		"Anemo / Traveler",
	)
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err = %v", len(records), err)
	}
	if len(records[0].Issues) != 0 || records[0].Character.Talent != (game.Talents{Auto: 10, Skill: 10, Burst: 10}) {
		t.Errorf("issues = %v, talents = %+v", records[0].Issues, records[0].Character.Talent)
	}
}

func TestCharacterScannerFlagsDisagreement(t *testing.T) {
	// The data says constellation 3 raises Hu Tao's skill, but the burst is blue.
	g := newCharacterGame(3)
	g.blue[0] = []int{2}
	s := characterScanner(g,
		"Pyro / Hu Tao", "Level 90 / 90", "Lv. 10", "Lv. 10", "Lv. 13",
		"Pyro / Hu Tao",
	)
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 1 || len(records[0].Issues) != 1 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestCharacterScannerIgnoresNonConstellationBonus(t *testing.T) {
	// Tartaglia's passive makes the normal attack blue without a constellation. The
	// blue isn't taken for a constellation boost, and his own passive pass then takes
	// the 1 level off.
	g := newCharacterGame(0)
	g.blue[0] = []int{0}
	s := characterScanner(g,
		"Hydro / Tartaglia", "Level 90 / 90", "Lv. 10", "Lv. 10", "Lv. 10",
		"Hydro / Tartaglia",
	)
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err = %v", len(records), err)
	}
	r := records[0]
	if len(r.Issues) != 0 || len(r.Notes) != 2 || len(r.Boosted) != 0 || r.Character.Talent.Auto != 9 {
		t.Errorf("issues = %v, notes = %v, boosted = %v, talents = %+v", r.Issues, r.Notes, r.Boosted, r.Character.Talent)
	}
}

func TestCharacterScannerSkipsAltSprint(t *testing.T) {
	g := newCharacterGame(0)
	s := characterScanner(g,
		"Hydro / Mona", "Level 1 / 20", "Lv. 1", "Lv. 1", "Lv. 1",
		"Hydro / Mona",
	)
	// Mona's burst is read from the fourth position, so blue there counts for her.
	g.blue[0] = []int{3}
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err = %v", len(records), err)
	}
	// Blue on the fourth position with no constellation is a mismatch only if it is read.
	if len(records[0].Issues) != 1 {
		t.Errorf("the burst at the fourth position should have been read as blue: %v", records[0].Issues)
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
		"Pyro / Hu Tao", "Level 1 / 20", "Lv. 1", "Lv. 1", "Lv. 1",
		"Hydro / Mona", "Level 1 / 20", "Lv. 1", "Lv. 1", "Lv. 1",
	)
	s.Limit = 2
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 2 {
		t.Errorf("records = %d, err = %v", len(records), err)
	}
}

func TestResolveBoosts(t *testing.T) {
	known := catalog.ConstellationBoosts{C3: game.TalentSkill, C5: game.TalentBurst}
	tests := []struct {
		name          string
		constellation int
		blue          [3]bool
		haveKnown     bool
		want          [3]bool
		note, issue   bool
	}{
		{"none", 0, [3]bool{}, true, [3]bool{}, false, false},
		{"c3 matches data", 3, [3]bool{false, true, false}, true, [3]bool{false, true, false}, false, false},
		{"c5 matches data", 5, [3]bool{false, true, true}, true, [3]bool{false, true, true}, false, false},
		{"c3 without data", 3, [3]bool{false, false, true}, false, [3]bool{false, false, true}, false, false},
		{"c3 contradicts data", 3, [3]bool{false, false, true}, true, [3]bool{false, false, true}, false, true},
		{"too many blue", 0, [3]bool{false, true, true}, true, [3]bool{}, false, true},
		{"too few blue", 5, [3]bool{false, true, false}, true, [3]bool{}, false, true},
		{"extra bonus on the normal attack", 3, [3]bool{true, true, false}, true, [3]bool{false, true, false}, true, false},
		{"normal attack bonus only", 0, [3]bool{true, false, false}, true, [3]bool{}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, note, issue := resolveBoosts(tt.constellation, tt.blue, known, tt.haveKnown)
			if got != tt.want || (note != "") != tt.note || (issue != "") != tt.issue {
				t.Errorf("got %v, note %q, issue %q; want %v, note %v, issue %v", got, note, issue, tt.want, tt.note, tt.issue)
			}
		})
	}
}

func TestCharacterScannerSavesPages(t *testing.T) {
	g := newCharacterGame(1, 0)
	dir := t.TempDir()
	s := characterScanner(g,
		"Pyro / Hu Tao", "Level 90 / 90", "Lv. 10", "Lv. 10", "Lv. 10",
		"Hydro / Mona", "Level 20 / 20", "Lv. 1", "Lv. 1", "Lv. 1",
		"Pyro / Hu Tao",
	)
	s.SaveFramesDir = filepath.Join(dir, "frames")
	records, err := s.Scan(t.Context())
	if err != nil || len(records) != 2 {
		t.Fatalf("records = %d, err = %v", len(records), err)
	}
	entries, err := os.ReadDir(s.SaveFramesDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{
		"000-HuTao-attributes.png", "000-HuTao-constellation.png", "000-HuTao-talents.png",
		"001-Mona-attributes.png", "001-Mona-constellation.png", "001-Mona-talents.png",
	}
	if !slices.Equal(names, want) {
		t.Errorf("saved %v, want %v", names, want)
	}
	if _, err := imaging.Load(filepath.Join(s.SaveFramesDir, want[0])); err != nil {
		t.Errorf("a saved page can't be read back: %v", err)
	}

	// Without a directory nothing is written.
	s.SaveFramesDir = ""
	g2 := newCharacterGame(0)
	if _, err := characterScanner(g2, "Pyro / Hu Tao", "Level 1 / 20", "Lv. 1", "Lv. 1", "Lv. 1", "Pyro / Hu Tao").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
}
