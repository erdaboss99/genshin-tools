package scan

import "github.com/erdaboss99/gensin-tools/internal/game"

// WeaponRecord is the result of scanning one weapon.
type WeaponRecord struct {
	Index  int         `json:"index"`
	Source string      `json:"source"`
	Weapon game.Weapon `json:"weapon"`
	// Issues are problems that may make the weapon wrong.
	Issues []string `json:"issues,omitempty"`
}

// CharacterRecord is the result of scanning one character.
type CharacterRecord struct {
	Index     int            `json:"index"`
	Source    string         `json:"source"`
	Character game.Character `json:"character"`
	// Issues are problems that may make the character wrong.
	Issues []string `json:"issues,omitempty"`
}

// MaterialRecord is the stack size of one inventory material.
type MaterialRecord struct {
	// Key is the GOOD material key, such as "MoraAndCrown".
	Key   string `json:"key"`
	Count int    `json:"count"`
}
