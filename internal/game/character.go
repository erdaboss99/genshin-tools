package game

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// TalentKind names one of a character's three upgradeable talents.
type TalentKind string

// The talents a constellation can raise.
const (
	TalentAuto  TalentKind = "auto"
	TalentSkill TalentKind = "skill"
	TalentBurst TalentKind = "burst"
)

// Talents are a character's talent levels. The game shows them with constellation
// bonuses added, and GOOD wants them without, which WithoutBoosts takes off.
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

// constellationBoost is how many levels a boosting constellation adds.
const constellationBoost = 3

// WithoutBoosts removes the levels the constellations add. The third constellation
// raises the talent c3, and the fifth raises c5, each by 3 levels. A talent name the
// character doesn't have leaves the levels as they are.
func (t Talents) WithoutBoosts(constellation int, c3, c5 TalentKind) Talents {
	take := func(kind TalentKind) {
		switch kind {
		case TalentAuto:
			t.Auto -= constellationBoost
		case TalentSkill:
			t.Skill -= constellationBoost
		case TalentBurst:
			t.Burst -= constellationBoost
		}
	}
	if constellation >= 3 {
		take(c3)
	}
	if constellation >= 5 {
		take(c5)
	}
	return t
}

// Elements the Traveler can have. GOOD names the Traveler by element, as in
// "TravelerAnemo".
var travelerElements = []string{"Anemo", "Geo", "Electro", "Dendro", "Hydro", "Pyro", "Cryo"}

// TravelerElements lists the elements the Traveler can have.
func TravelerElements() []string { return slices.Clone(travelerElements) }

// TravelerKey returns the GOOD character key of the Traveler with this element.
func TravelerKey(element string) CharacterKey {
	return CharacterKey(string(CharacterTraveler) + element)
}

// IsTraveler reports whether the key is a Traveler of any element.
func (k CharacterKey) IsTraveler() bool {
	return strings.HasPrefix(string(k), string(CharacterTraveler))
}
