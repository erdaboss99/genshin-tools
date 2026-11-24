package game

import (
	"errors"
	"fmt"
)

// Talents are a character's talent levels as shown in game, without constellation bonuses.
type Talents struct {
	Auto  int `json:"auto"`
	Skill int `json:"skill"`
	Burst int `json:"burst"`
}

// Character is a single scanned character.
type Character struct {
	Key           CharacterKey `json:"key"`
	Level         Level        `json:"level"`
	Ascension     int          `json:"ascension"`
	Constellation int          `json:"constellation"`
	Talent        Talents      `json:"talent"`
}

// Validate checks the character against the game's rules, returning every problem found.
func (c Character) Validate() error {
	var errs []error
	if c.Key == "" {
		errs = append(errs, errors.New("character is unknown"))
	}
	if err := checkLevelAscension(c.Level, c.Ascension); err != nil {
		errs = append(errs, err)
	}
	if c.Constellation < 0 || c.Constellation > 6 {
		errs = append(errs, fmt.Errorf("constellation %d is out of range", c.Constellation))
	}
	for name, level := range map[string]int{"auto": c.Talent.Auto, "skill": c.Talent.Skill, "burst": c.Talent.Burst} {
		if level < 1 || level > 15 {
			errs = append(errs, fmt.Errorf("%s talent level %d is out of range", name, level))
		}
	}
	return errors.Join(errs...)
}
