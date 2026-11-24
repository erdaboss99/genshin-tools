package scan

import (
	"context"
	"image"
	"image/color"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/erdaboss99/gensin-tools/internal/catalog"
	"github.com/erdaboss99/gensin-tools/internal/fuzzy"
	"github.com/erdaboss99/gensin-tools/internal/game"
	"github.com/erdaboss99/gensin-tools/internal/imaging"
	"github.com/erdaboss99/gensin-tools/internal/layout"
	"github.com/erdaboss99/gensin-tools/ocr"
)

// The weapon panel's reading rules follow the approach of Inventory Kamera
// (https://github.com/Andrewthe13th/Inventory_Kamera, MIT): the level is shown as
// "level/cap", where the cap gives the ascension, and the rarity is the colour of
// the name banner.

const levelAllowed = "Lv.0123456789/"

// weaponTabMaterials are enhancement ores listed after the weapons in the same
// inventory tab, which look like weapons to the grid walker.
var weaponTabMaterials = []string{"Enhancement Ore", "Fine Enhancement Ore", "Mystic Enhancement Ore"}

// nameplateColors are the average banner colours by rarity, with black for "none".
var nameplateColors = []struct {
	rarity game.Rarity
	color  color.RGBA
}{
	{0, color.RGBA{0, 0, 0, 255}},
	{1, color.RGBA{114, 119, 138, 255}},
	{2, color.RGBA{42, 143, 114, 255}},
	{3, color.RGBA{81, 127, 203, 255}},
	{4, color.RGBA{161, 86, 224, 255}},
	{5, color.RGBA{188, 105, 50, 255}},
}

var levelPattern = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)

// WeaponParser extracts weapons from inventory frames.
type WeaponParser struct {
	Layout  layout.Weapons
	OCR     ocr.Recognizer
	Catalog *catalog.Catalog
	// TravelerName is the player's chosen name, which the game shows instead of
	// "Traveler" on equipped weapons.
	TravelerName string
}

// IsUnrelated reports whether the frame shows an enhancement ore, not a weapon.
func (p WeaponParser) IsUnrelated(ctx context.Context, frame *image.RGBA) (bool, error) {
	r, err := newReader(ctx, p.OCR, frame)
	if err != nil {
		return false, err
	}
	name, err := r.readName(p.Layout.Name)
	if err != nil {
		return false, err
	}
	return isUnrelatedWeapon(name), nil
}

func isUnrelatedWeapon(name string) bool {
	_, ok := fuzzy.Best(name, weaponTabMaterials, fuzzy.Identity, minUnrelated)
	return ok
}

// Parse reads one weapon. Unreadable fields become Issues rather than errors; an
// error means OCR could not run at all. skipped is true for enhancement ores.
func (p WeaponParser) Parse(ctx context.Context, frame *image.RGBA) (record WeaponRecord, skipped bool, err error) {
	r, err := newReader(ctx, p.OCR, frame)
	if err != nil {
		record.Issues = append(record.Issues, err.Error())
		return record, false, nil
	}

	if record.Raw.Name, err = r.readName(p.Layout.Name); err != nil {
		return record, false, err
	}
	if isUnrelatedWeapon(record.Raw.Name) {
		return record, true, nil
	}

	w := &record.Weapon
	if match, ok := p.Catalog.MatchWeapon(record.Raw.Name); ok {
		w.Key = game.WeaponKey(game.ToKey(match.Item))
	} else {
		record.issue("unrecognized weapon name %q", record.Raw.Name)
	}
	record.Rarity = weaponRarity(imaging.MeanColor(frame, r.win.Rect(p.Layout.Nameplate)))
	w.Lock = iconIsOn(imaging.MeanColor(frame, r.win.Rect(p.Layout.LockIcon)))

	if record.Raw.Level, err = r.read(p.Layout.Level, ocr.ModeLine, levelAllowed); err != nil {
		return record, false, err
	}
	if level, ascension, ok := parseLevelCap(record.Raw.Level); ok {
		w.Level, w.Ascension = level, ascension
	} else {
		record.issue("unreadable level %q", record.Raw.Level)
	}

	refinement, raw, err := p.readRefinement(r)
	if err != nil {
		return record, false, err
	}
	record.Raw.Refinement = raw
	if refinement > 0 {
		w.Refinement = refinement
	} else {
		record.issue("unreadable refinement %q", record.Raw.Refinement)
	}

	if record.Raw.Equipped, err = r.read(p.Layout.Equipped, ocr.ModeLine, ""); err != nil {
		return record, false, err
	}
	w.Location = resolveLocation(p.Catalog, p.TravelerName, record.Raw.Equipped)

	if verr := w.Validate(); verr != nil {
		for line := range strings.SplitSeq(verr.Error(), "\n") {
			if !slices.Contains(record.Issues, line) {
				record.issue("%s", line)
			}
		}
	}
	return record, false, nil
}

// readRefinement reads the refinement badge, a single digit on a coloured square. The
// badge is grey for most weapons but orange for others, and the darkest colour
// channel that the rest of the scanner uses can't separate the orange one's yellow
// digit, so the green channel goes first. Later attempts only run after a failed
// read: the other channel, then single-character mode. refinement is 0 when no
// attempt gave a digit from 1 to 5, and raw is what the first attempt read.
func (p WeaponParser) readRefinement(r reader) (refinement int, raw string, err error) {
	rect := r.win.Rect(p.Layout.Refinement)
	scale := imaging.OCRScale(r.win.Width)
	attempts := []struct {
		mode    ocr.Mode
		scale   int
		channel imaging.Channel
	}{
		{ocr.ModeLine, scale, imaging.Green},
		{ocr.ModeLine, scale, imaging.Darkest},
		{ocr.ModeChar, scale, imaging.Green},
		{ocr.ModeChar, scale * 2, imaging.Darkest},
	}
	for i, attempt := range attempts {
		text, err := r.readRectChannel(rect, attempt.mode, "12345", attempt.scale, attempt.channel)
		if err != nil {
			return 0, raw, err
		}
		if i == 0 {
			raw = text
		}
		if n, convErr := strconv.Atoi(keepDigits(text)); convErr == nil && n >= 1 && n <= 5 {
			return n, raw, nil
		}
	}
	return 0, raw, nil
}

// parseLevelCap reads "Lv. 80/90", as shown for weapons and characters. The number after the slash is the level cap of
// the current ascension, so it gives the ascension: a weapon at 20/40 has been
// ascended once, while one at 20/20 has not.
func parseLevelCap(text string) (level game.Level, ascension int, ok bool) {
	m := levelPattern.FindStringSubmatch(itemCountDigitFixes.Replace(text))
	if m == nil {
		return 0, 0, false
	}
	value, err1 := strconv.Atoi(m[1])
	limit, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	// OCR often misreads the last digit of the cap, which is always a multiple of ten.
	limit = (limit + 5) / 10 * 10
	ascension = game.AscensionForCap(game.Level(limit))
	if ascension < 0 || value < 1 || value > limit {
		return 0, 0, false
	}
	return game.Level(value), ascension, true
}

// weaponRarity picks the rarity whose banner colour is closest to c.
func weaponRarity(c color.RGBA) game.Rarity {
	best, bestDistance := game.Rarity(0), -1
	for _, candidate := range nameplateColors {
		dr, dg, db := int(c.R)-int(candidate.color.R), int(c.G)-int(candidate.color.G), int(c.B)-int(candidate.color.B)
		if d := dr*dr + dg*dg + db*db; bestDistance < 0 || d < bestDistance {
			best, bestDistance = candidate.rarity, d
		}
	}
	return best
}
