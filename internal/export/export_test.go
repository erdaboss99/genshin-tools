package export

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/erdaboss99/gensin-tools/internal/game"
)

func TestNewGOOD(t *testing.T) {
	good := game.Artifact{
		SetKey: "GladiatorsFinale", Slot: game.SlotSands, Rarity: 5, Level: 20,
		MainStat: game.StatATKPercent, Location: "HuTao", Lock: true, AstralMark: true, ElixirCrafted: true,
		Substats: []game.Substat{
			{Key: game.StatCritRate, Value: 3.9}, {Key: game.StatDEF, Value: 23}, {Key: game.StatEnergyRecharge, Value: 6.5},
		},
		Unactivated: []game.Substat{{Key: game.StatHP, Value: 299}},
	}
	bad := game.Artifact{Slot: game.SlotSands, Rarity: 5}

	doc, rejected := NewGOOD([]game.Artifact{good, bad})
	if len(rejected) != 1 || !strings.Contains(rejected[0].Error(), "artifact 1") {
		t.Errorf("rejected = %v", rejected)
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"format":"GOOD","version":2,"source":"gensin-tools","artifacts":[{"setKey":"GladiatorsFinale","slotKey":"sands","level":20,"rarity":5,"mainStatKey":"atk_","location":"HuTao","lock":true,"substats":[{"key":"critRate_","value":3.9},{"key":"def","value":23},{"key":"enerRech_","value":6.5}],"unactivatedSubstats":[{"key":"hp","value":299}],"astralMark":true,"elixirCrafted":true}]}`
	if string(raw) != want {
		t.Errorf("got  %s\nwant %s", raw, want)
	}
}

func TestNewGOODEmitsEmptyArrays(t *testing.T) {
	doc, _ := NewGOOD(nil)
	raw, _ := json.Marshal(doc)
	if !strings.Contains(string(raw), `"artifacts":[]`) {
		t.Errorf("nil artifacts must encode as []: %s", raw)
	}
	doc, _ = NewGOOD([]game.Artifact{{SetKey: "X", Slot: game.SlotFlower, Rarity: 2, MainStat: game.StatHP}})
	raw, _ = json.Marshal(doc)
	if !strings.Contains(string(raw), `"substats":[]`) {
		t.Errorf("nil substats must encode as []: %s", raw)
	}
}

func TestPaimonMoe(t *testing.T) {
	completed := []game.CompletedAchievement{{ID: 80091, Category: 0}, {ID: 80127, Category: 0}, {ID: 81000, Category: 4}}
	raw, err := PaimonMoe(completed, []byte(`{"achievement":{"9":{"1":true}},"wish-counter":{"pity":12}}`))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Achievement map[string]map[string]bool `json:"achievement"`
		Wish        map[string]int             `json:"wish-counter"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Achievement["0"]["80127"] || !doc.Achievement["4"]["81000"] {
		t.Errorf("achievement = %v", doc.Achievement)
	}
	if doc.Achievement["9"] != nil {
		t.Error("stale achievements were kept")
	}
	if doc.Wish["pity"] != 12 {
		t.Error("unrelated data was lost")
	}
}

func TestSeelieMe(t *testing.T) {
	raw, err := SeelieMe([]game.CompletedAchievement{{ID: 5, Category: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Achievements map[string]struct {
			Done bool `json:"done"`
		} `json:"achievements"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Achievements["5"].Done {
		t.Errorf("achievements = %+v", doc.Achievements)
	}
}

func TestMergeRejectsCorruptExisting(t *testing.T) {
	if _, err := SeelieMe(nil, []byte(`[not json`)); err == nil {
		t.Error("corrupt merge input accepted")
	}
}

func TestReadPaimonMoe(t *testing.T) {
	raw := []byte(`{"achievement":{"4":{"81000":true},"0":{"80127":true,"80091":true,"80092":false}},"wish-counter":{}}`)
	got, err := ReadPaimonMoe(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []game.CompletedAchievement{{ID: 80091, Category: 0}, {ID: 80127, Category: 0}, {ID: 81000, Category: 4}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	if _, err := ReadPaimonMoe([]byte(`{"wish-counter":{}}`)); err == nil {
		t.Error("an export without achievements must be rejected")
	}
	if _, err := ReadPaimonMoe([]byte(`{"achievement":{"x":{}}}`)); err == nil {
		t.Error("a non-numeric category must be rejected")
	}
}

func TestBuildGOODAllSections(t *testing.T) {
	doc, rejected := BuildGOOD(GOODInput{
		Weapons: []game.Weapon{
			{Key: "MistsplitterReforged", Level: 90, Ascension: 6, Refinement: 1, Location: "Kamisato Ayaka"},
			{Key: "", Level: 1, Refinement: 1},
		},
		Characters: []game.Character{{Key: "HuTao", Level: 90, Ascension: 6, Constellation: 1, Talent: game.Talents{Auto: 10, Skill: 10, Burst: 10}}},
		Materials:  map[string]int{"MoraAndCrown": 12, "Bad": 0},
	})
	if len(rejected) != 2 {
		t.Errorf("rejected = %v, want the unknown weapon and the empty material", rejected)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		`"weapons":[{"key":"MistsplitterReforged","level":90,"ascension":6,"refinement":1,"location":"Kamisato Ayaka"}]`,
		`"characters":[{"key":"HuTao","level":90,"ascension":6,"constellation":1,"talent":{"auto":10,"skill":10,"burst":10}}]`,
		`"materials":{"MoraAndCrown":12}`,
		`"artifacts":[]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestBuildGOODOmitsEmptySections(t *testing.T) {
	doc, _ := NewGOOD(nil)
	raw, _ := json.Marshal(doc)
	for _, key := range []string{"weapons", "characters", "materials"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("%s must be omitted when empty: %s", key, raw)
		}
	}
}
