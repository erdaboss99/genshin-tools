package scan

import (
	"image/color"
	"testing"

	"github.com/erdaboss99/gensin-tools/internal/catalog"
	"github.com/erdaboss99/gensin-tools/internal/game"
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
