package scan

import (
	"fmt"

	"github.com/erdaboss99/gensin-tools/internal/game"
)

// RawWeaponText preserves what OCR read, for diagnosing bad results.
type RawWeaponText struct {
	Name       string `json:"name"`
	Level      string `json:"level"`
	Refinement string `json:"refinement"`
	Equipped   string `json:"equipped,omitempty"`
}

// WeaponRecord is the result of scanning one weapon.
type WeaponRecord struct {
	Index  int         `json:"index"`
	Source string      `json:"source"`
	Weapon game.Weapon `json:"weapon"`
	// Rarity is the weapon's star count, read from its name banner. It is not part
	// of GOOD and only decides which weapons the report keeps.
	Rarity game.Rarity `json:"rarity,omitempty"`
	// Issues are problems that may make the weapon wrong.
	Issues []string `json:"issues,omitempty"`
	// Notes are things worth knowing that don't make the weapon wrong. A weapon that
	// repeats the previous one is usually another copy of the same weapon.
	Notes []string      `json:"notes,omitempty"`
	Raw   RawWeaponText `json:"raw"`
}

func (r *WeaponRecord) issue(format string, args ...any) {
	r.Issues = append(r.Issues, fmt.Sprintf(format, args...))
}

// RawCharacterText preserves what OCR read, for diagnosing bad results.
type RawCharacterText struct {
	Header  string   `json:"header"`
	Level   string   `json:"level"`
	Talents []string `json:"talents,omitempty"`
}

// CharacterRecord is the result of scanning one character.
type CharacterRecord struct {
	Index     int            `json:"index"`
	Source    string         `json:"source"`
	Character game.Character `json:"character"`
	// Element is the element shown beside the name. For the Traveler it is part of
	// the key.
	Element string `json:"element,omitempty"`
	// Issues are problems that may make the character wrong.
	Issues []string `json:"issues,omitempty"`
	// Notes are corrections and other things worth knowing that don't make the
	// character wrong.
	Notes []string         `json:"notes,omitempty"`
	Raw   RawCharacterText `json:"raw"`
}

func (r *CharacterRecord) issue(format string, args ...any) {
	r.Issues = append(r.Issues, fmt.Sprintf(format, args...))
}

func (r *CharacterRecord) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// MaterialRecord is the stack size of one inventory material.
type MaterialRecord struct {
	// Key is the GOOD material key, such as "MoraAndCrown".
	Key   string `json:"key"`
	Count int    `json:"count"`
}
