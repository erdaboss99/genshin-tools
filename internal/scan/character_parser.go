package scan

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"

	"github.com/erdaboss99/genshin-tools/internal/catalog"
	"github.com/erdaboss99/genshin-tools/internal/fuzzy"
	"github.com/erdaboss99/genshin-tools/internal/game"
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

// cyanMinShare is how much of a talent's level box must be light blue for it to count
// as boosted. Boosted levels measure 7% to 11%, and unboosted ones 1% or less, even on
// a teal background.
const cyanMinShare = 0.03

// talentBoosted reports whether a talent's level is drawn in the light blue the game
// uses when a constellation raises it.
func talentBoosted(frame *image.RGBA, rect image.Rectangle) bool {
	rect = rect.Intersect(frame.Bounds())
	cyan, total := 0, 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			p := frame.RGBAAt(x, y)
			total++
			// Bright, since a teal or blue background has the same hue but is darker.
			if p.G > 200 && p.B > 210 && p.R < 150 {
				cyan++
			}
		}
	}
	return total > 0 && float64(cyan)/float64(total) >= cyanMinShare
}

// resolveBoosts works out which talents a character's constellations raised, from
// the talent levels that are drawn in blue and, when there is any, the constellation
// data. The blue says what is on screen, and the data says what should be there, so
// they check each other. Bonuses that don't come from constellations, such as
// Tartaglia's passive on the normal attack, are blue too and are left out.
func resolveBoosts(constellation int, blue [3]bool, known catalog.ConstellationBoosts, haveKnown bool) (raised [3]bool, note string, issue string) {
	index := map[game.TalentKind]int{game.TalentAuto: 0, game.TalentSkill: 1, game.TalentBurst: 2}
	want := 0
	if constellation >= 3 {
		want = 1
	}
	if constellation >= 5 {
		want = 2
	}
	var expected [3]bool
	if haveKnown {
		if constellation >= 3 {
			expected[index[known.C3]] = true
		}
		if constellation >= 5 {
			expected[index[known.C5]] = true
		}
	}
	count := func(b [3]bool) (n int) {
		for _, v := range b {
			if v {
				n++
			}
		}
		return n
	}

	switch {
	case count(blue) == want && (!haveKnown || blue == expected):
		return blue, "", ""
	case count(blue) == want:
		return blue, "", "the talents drawn in blue don't match the constellation data"
	case count(blue) == want+1 && blue[0] && !expected[0]:
		// The normal attack is also raised by something that isn't a constellation.
		blue[0] = false
		if count(blue) == want && (!haveKnown || blue == expected) {
			return blue, "the normal attack has a bonus that isn't from a constellation", ""
		}
	}
	return [3]bool{}, "", fmt.Sprintf("%d talents are drawn in blue, but constellation %d raises %d", count(blue), constellation, want)
}
