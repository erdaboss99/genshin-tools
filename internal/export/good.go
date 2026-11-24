package export

import (
	"fmt"

	"github.com/erdaboss99/gensin-tools/internal/game"
)

const (
	goodFormat = "GOOD"
	goodSource = "gensin-tools"
	// goodVersion is the GOOD format version emitted. Version 2 has the widest tool
	// support, and tools ignore fields they don't recognize.
	goodVersion = 2
)

// GOOD is the Genshin Open Object Description document. Artifacts are always
// present, while the other sections appear only when they have content, so an
// importer doesn't mistake an empty section for "delete everything".
type GOOD struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	Source     string           `json:"source"`
	Artifacts  []GOODArtifact   `json:"artifacts"`
	Weapons    []game.Weapon    `json:"weapons,omitempty"`
	Characters []game.Character `json:"characters,omitempty"`
	Materials  map[string]int   `json:"materials,omitempty"`
}

// GOODInput is everything that can go into a GOOD document.
type GOODInput struct {
	Artifacts  []game.Artifact
	Weapons    []game.Weapon
	Characters []game.Character
	Materials  map[string]int
}

// GOODArtifact is one artifact in GOOD form.
type GOODArtifact struct {
	SetKey      game.SetKey       `json:"setKey"`
	SlotKey     game.Slot         `json:"slotKey"`
	Level       game.Level        `json:"level"`
	Rarity      game.Rarity       `json:"rarity"`
	MainStatKey game.Stat         `json:"mainStatKey"`
	Location    game.CharacterKey `json:"location"`
	Lock        bool              `json:"lock"`
	Substats    []game.Substat    `json:"substats"`
	Unactivated []game.Substat    `json:"unactivatedSubstats,omitempty"`
	AstralMark  bool              `json:"astralMark,omitempty"`
	Elixir      bool              `json:"elixirCrafted,omitempty"`
}

// NewGOOD builds a GOOD document. Artifacts failing validation are left out, since
// importers would silently replace bad fields with defaults.
func NewGOOD(artifacts []game.Artifact) (GOOD, []error) {
	return BuildGOOD(GOODInput{Artifacts: artifacts})
}

// BuildGOOD builds a GOOD document from every section. Items failing validation are
// left out and returned as errors, since importers would silently replace bad fields
// with defaults. Materials with a non-positive count or no key are dropped the same way.
func BuildGOOD(in GOODInput) (GOOD, []error) {
	doc := GOOD{Format: goodFormat, Version: goodVersion, Source: goodSource, Artifacts: []GOODArtifact{}}
	var rejected []error
	for i, a := range in.Artifacts {
		if err := a.Validate(); err != nil {
			rejected = append(rejected, fmt.Errorf("artifact %d: %w", i, err))
			continue
		}
		doc.Artifacts = append(doc.Artifacts, newGOODArtifact(a))
	}
	for i, w := range in.Weapons {
		if err := w.Validate(); err != nil {
			rejected = append(rejected, fmt.Errorf("weapon %d: %w", i, err))
			continue
		}
		doc.Weapons = append(doc.Weapons, w)
	}
	for i, c := range in.Characters {
		if err := c.Validate(); err != nil {
			rejected = append(rejected, fmt.Errorf("character %d: %w", i, err))
			continue
		}
		doc.Characters = append(doc.Characters, c)
	}
	for key, count := range in.Materials {
		if key == "" || count <= 0 {
			rejected = append(rejected, fmt.Errorf("material %q: count %d is not usable", key, count))
			continue
		}
		if doc.Materials == nil {
			doc.Materials = make(map[string]int)
		}
		doc.Materials[key] = count
	}
	return doc, rejected
}

func newGOODArtifact(a game.Artifact) GOODArtifact {
	substats := a.Substats
	if substats == nil {
		substats = []game.Substat{}
	}
	return GOODArtifact{
		SetKey:      a.SetKey,
		SlotKey:     a.Slot,
		Level:       a.Level,
		Rarity:      a.Rarity,
		MainStatKey: a.MainStat,
		Location:    a.Location,
		Lock:        a.Lock,
		Substats:    substats,
		Unactivated: a.Unactivated,
		AstralMark:  a.AstralMark,
		Elixir:      a.ElixirCrafted,
	}
}
