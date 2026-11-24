package scan

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/erdaboss99/gensin-tools/internal/catalog"
	"github.com/erdaboss99/gensin-tools/internal/fuzzy"
	"github.com/erdaboss99/gensin-tools/internal/game"
)

// The character screens are read the way Inventory Kamera
// (https://github.com/Andrewthe13th/Inventory_Kamera, MIT) reads them: the header
// shows "Element / Name", a constellation that isn't unlocked has a white
// "Activate" button, and a talent's level is the last line of its panel.

const (
	minElementScore = 70
	// activateMinBrightness is how bright every channel of the "Activate" button
	// is when the constellation isn't unlocked yet.
	activateMinBrightness = 190
	talentMinLevel        = 1
	talentMaxLevel        = 15
)

// characterElements are the elements shown beside a character's name.
var characterElements = []string{"Pyro", "Hydro", "Anemo", "Electro", "Dendro", "Cryo", "Geo"}

// altSprintCharacters have an alternate sprint talent listed before the burst, so
// the burst is one position further down.
var altSprintCharacters = []string{"Mona", "Kamisato Ayaka"}

// characterHeader is the result of reading the header.
type characterHeader struct {
	Key     game.CharacterKey
	Name    string // the catalog name, for data lookups
	Element string
}

// parseCharacterHeader reads text such as "Pyro / Hu Tao". Names that are long enough
// to wrap onto two lines come out with the slash on the second line, so the text is
// joined first. The Traveler has the player's chosen name, and their key depends on
// the element.
func parseCharacterHeader(text string, cat *catalog.Catalog, travelerName string) (characterHeader, bool) {
	text = strings.Join(strings.Fields(text), " ")
	left, right, found := strings.Cut(text, "/")
	if !found {
		return characterHeader{}, false
	}
	var h characterHeader
	// A long name wraps, which leaves its first part beside the element, as in
	// "Cryo Kamisato / Ayaka", so the other words on the left belong to the name.
	var nameStart []string
	for _, word := range strings.Fields(left) {
		if m, ok := fuzzy.Best(word, characterElements, fuzzy.Identity, minElementScore); ok && h.Element == "" {
			h.Element = m.Item
			continue
		}
		nameStart = append(nameStart, word)
	}
	if h.Element == "" {
		return characterHeader{}, false
	}
	right = strings.TrimSpace(right)
	if travelerName != "" && fuzzy.Ratio(right, travelerName) >= minOverrideScore {
		h.Key, h.Name = game.TravelerKey(h.Element), "Traveler"
		return h, true
	}
	m, ok := cat.MatchCharacter(right)
	if len(nameStart) > 0 {
		if wrapped, found := cat.MatchCharacter(strings.Join(append(nameStart, right), " ")); found && (!ok || wrapped.Score > m.Score) {
			m, ok = wrapped, true
		}
	}
	if !ok {
		return characterHeader{}, false
	}
	h.Name = m.Item
	if m.Item == "Traveler" {
		h.Key = game.TravelerKey(h.Element)
	} else {
		h.Key = game.CharacterKey(game.ToKey(m.Item))
	}
	return h, true
}

// parseTalentLevel reads the level from a talent panel, whose last line is the level.
func parseTalentLevel(text string) (int, bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	level, err := strconv.Atoi(keepDigits(lines[len(lines)-1]))
	if err != nil || level < talentMinLevel || level > talentMaxLevel {
		return 0, false
	}
	return level, true
}

// constellationLocked reports whether an "Activate" button is showing, which means
// the constellation isn't unlocked.
func constellationLocked(c color.RGBA) bool {
	return c.R >= activateMinBrightness && c.G >= activateMinBrightness && c.B >= activateMinBrightness
}

func hasAltSprint(name string) bool {
	for _, n := range altSprintCharacters {
		if n == name {
			return true
		}
	}
	return false
}

// applyTartagliaBuff takes off the extra normal attack level that Tartaglia's
// passive gives while he is in the party. The game shows it on every party member
// and on him once he reaches ascension 4. The party is assumed to be the first four
// characters, as the character screen lists it first. It reports whether it applied.
func applyTartagliaBuff(records []CharacterRecord) bool {
	const partySize = 4
	for i := range records {
		c := &records[i].Character
		if c.Key != "Tartaglia" {
			continue
		}
		if c.Ascension < 4 {
			return false
		}
		targets := []int{i}
		if i < partySize {
			targets = targets[:0]
			for j := range min(partySize, len(records)) {
				targets = append(targets, j)
			}
		}
		for _, j := range targets {
			records[j].Character.Talent.Auto--
			records[j].note("took 1 off the normal attack level for Tartaglia's passive")
		}
		return true
	}
	return false
}
