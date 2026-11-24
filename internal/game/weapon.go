package game

import (
	"errors"
	"fmt"
)

// WeaponKey is a GOOD weapon key, such as "MistsplitterReforged".
type WeaponKey string

// ascensionCaps is the level cap at each ascension phase. It applies to weapons and
// characters alike.
var ascensionCaps = [...]Level{20, 40, 50, 60, 70, 80, 90}

// maxAscension is the highest ascension phase.
const maxAscension = len(ascensionCaps) - 1

// checkLevelAscension reports whether a level can exist at an ascension phase. A
// character or weapon at a level cap can be on either side of the ascension.
func checkLevelAscension(level Level, ascension int) error {
	if ascension < 0 || ascension > maxAscension {
		return fmt.Errorf("ascension %d is out of range", ascension)
	}
	if level < 1 || level > ascensionCaps[ascension] {
		return fmt.Errorf("level %d is impossible at ascension %d", level, ascension)
	}
	if ascension > 0 && level < ascensionCaps[ascension-1] {
		return fmt.Errorf("level %d is too low for ascension %d", level, ascension)
	}
	return nil
}

// Weapon is a single scanned weapon.
type Weapon struct {
	Key        WeaponKey    `json:"key"`
	Level      Level        `json:"level"`
	Ascension  int          `json:"ascension"`
	Refinement int          `json:"refinement"`
	Location   CharacterKey `json:"location,omitempty"`
	Lock       bool         `json:"lock,omitempty"`
}

// Validate checks the weapon against the game's rules, returning every problem found.
func (w Weapon) Validate() error {
	var errs []error
	if w.Key == "" {
		errs = append(errs, errors.New("weapon is unknown"))
	}
	if err := checkLevelAscension(w.Level, w.Ascension); err != nil {
		errs = append(errs, err)
	}
	if w.Refinement < 1 || w.Refinement > 5 {
		errs = append(errs, fmt.Errorf("refinement %d is out of range", w.Refinement))
	}
	return errors.Join(errs...)
}
