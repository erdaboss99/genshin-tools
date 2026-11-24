package scan

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/erdaboss99/gensin-tools/control"
	"github.com/erdaboss99/gensin-tools/internal/catalog"
	"github.com/erdaboss99/gensin-tools/internal/game"
	"github.com/erdaboss99/gensin-tools/internal/imaging"
	"github.com/erdaboss99/gensin-tools/internal/layout"
	"github.com/erdaboss99/gensin-tools/ocr"
)

const (
	// maxCharacters stops a scan that never comes back to its first character.
	maxCharacters = 200
	// readAttempts is how many times a field is read again while the screen redraws.
	readAttempts = 8
	// unreadableLimit is how many characters in a row may lack a readable name before
	// the scan gives up, as that means the character screen isn't open.
	unreadableLimit = 3

	tabDelay      = 700 * time.Millisecond
	nodeDelay     = 600 * time.Millisecond
	nextDelay     = 700 * time.Millisecond
	overlayDelay  = 500 * time.Millisecond
	rereadDelay   = 150 * time.Millisecond
	constellCount = 6
	talentCount   = 3
)

// CharacterScanner walks the character screen with the game open on it. Unlike the
// inventory scans it has to click through three pages of each character, so it
// reads as it goes instead of producing frames for later.
type CharacterScanner struct {
	Game    control.Game
	Layout  layout.Characters
	OCR     ocr.Recognizer
	Catalog *catalog.Catalog
	// TravelerName is the player's chosen name, which the game shows instead of
	// "Traveler".
	TravelerName string
	Pace         Pace
	// Limit stops after this many characters. Zero means all of them.
	Limit int
	Log   *slog.Logger
}

// Scan reads every character, starting from the one on screen and moving on until it
// comes back to the first. Records read before a failure are returned with it.
func (s CharacterScanner) Scan(ctx context.Context) ([]CharacterRecord, error) {
	width, height := s.Game.Size()
	win, err := layout.NewWindow(width, height)
	if err != nil {
		return nil, err
	}

	var (
		records    []CharacterRecord
		first      game.CharacterKey
		seen       = make(map[game.CharacterKey]bool)
		unreadable int
	)
	for position := 0; position < maxCharacters; position++ {
		record, err := s.scanCharacter(ctx, win, first)
		if err != nil {
			return s.finish(records), err
		}
		key := record.Character.Key
		switch {
		case key == "":
			if unreadable++; unreadable >= unreadableLimit {
				return s.finish(records), errors.New("cannot read character names: open the character screen, run calibrate, or raise --pace")
			}
		case key == first:
			return s.finish(records), nil
		case seen[key]:
			record.issue("seen already: the walk may have skipped ahead")
		default:
			unreadable = 0
			seen[key] = true
			if first == "" {
				first = key
			}
		}
		record.Index = len(records)
		record.Source = fmt.Sprintf(liveSourceFormat, record.Index)
		records = append(records, record)
		if s.Log != nil {
			s.Log.Info("scanned character", "character", key, "issues", len(record.Issues))
		}
		if s.Limit > 0 && len(records) >= s.Limit {
			return s.finish(records), nil
		}
		if err := s.click(ctx, win, s.Layout.NextCharacter, nextDelay); err != nil {
			return s.finish(records), err
		}
	}
	return s.finish(records), fmt.Errorf("did not return to the first character after %d characters", maxCharacters)
}

func (s CharacterScanner) finish(records []CharacterRecord) []CharacterRecord {
	applyTartagliaBuff(records)
	return records
}

// scanCharacter reads the character on screen. When it turns out to be first, the
// walk has come full circle and nothing else is read.
func (s CharacterScanner) scanCharacter(ctx context.Context, win layout.Window, first game.CharacterKey) (CharacterRecord, error) {
	var record CharacterRecord
	if err := s.click(ctx, win, s.Layout.AttributesTab, tabDelay); err != nil {
		return record, err
	}

	header, ok, err := s.readHeader(ctx, &record)
	if err != nil {
		return record, err
	}
	if !ok {
		record.issue("unreadable name %q", record.Raw.Header)
		return record, nil
	}
	c := &record.Character
	c.Key, record.Element = header.Key, header.Element
	if c.Key == first {
		return record, nil
	}

	if err := s.readLevel(ctx, &record); err != nil {
		return record, err
	}
	if err := s.readConstellation(ctx, win, &record); err != nil {
		return record, err
	}
	if err := s.readTalents(ctx, win, &record, header.Name); err != nil {
		return record, err
	}

	if verr := c.Validate(); verr != nil {
		for line := range strings.SplitSeq(verr.Error(), "\n") {
			if !slices.Contains(record.Issues, line) {
				record.issue("%s", line)
			}
		}
	}
	return record, nil
}

func (s CharacterScanner) readHeader(ctx context.Context, record *CharacterRecord) (characterHeader, bool, error) {
	for attempt := range readAttempts {
		r, err := s.reader(ctx)
		if err != nil {
			return characterHeader{}, false, err
		}
		text, err := r.read(s.Layout.Header, ocr.ModeBlock, "")
		if err != nil {
			return characterHeader{}, false, err
		}
		record.Raw.Header = text
		if header, ok := parseCharacterHeader(text, s.Catalog, s.TravelerName); ok {
			return header, true, nil
		}
		if attempt < readAttempts-1 {
			if err := s.Pace.sleep(ctx, rereadDelay); err != nil {
				return characterHeader{}, false, err
			}
		}
	}
	return characterHeader{}, false, nil
}

func (s CharacterScanner) readLevel(ctx context.Context, record *CharacterRecord) error {
	for attempt := range readAttempts {
		r, err := s.reader(ctx)
		if err != nil {
			return err
		}
		text, err := r.read(s.Layout.Level, ocr.ModeLine, levelAllowed)
		if err != nil {
			return err
		}
		record.Raw.Level = text
		if level, ascension, ok := parseLevelCap(text); ok {
			record.Character.Level, record.Character.Ascension = level, ascension
			return nil
		}
		if attempt < readAttempts-1 {
			if err := s.Pace.sleep(ctx, rereadDelay); err != nil {
				return err
			}
		}
	}
	record.issue("unreadable level %q", record.Raw.Level)
	return nil
}

// readConstellation opens each constellation in turn. One that isn't unlocked shows a
// white "Activate" button, so the count is how many come before the first of those.
func (s CharacterScanner) readConstellation(ctx context.Context, win layout.Window, record *CharacterRecord) error {
	if err := s.click(ctx, win, s.Layout.ConstellationTab, tabDelay); err != nil {
		return err
	}
	count := 0
	for ; count < constellCount; count++ {
		if err := s.click(ctx, win, s.Layout.Constellations.At(count), nodeDelay); err != nil {
			return err
		}
		frame, err := s.Game.Capture()
		if err != nil {
			return err
		}
		if constellationLocked(imaging.MeanColor(frame, win.Rect(s.Layout.Activate))) {
			break
		}
	}
	record.Character.Constellation = count
	return s.closeOverlay(ctx)
}

// readTalents reads the three talent levels from the talents page, which lists them
// all, and notes which are drawn in blue. Those are the ones constellations raised.
func (s CharacterScanner) readTalents(ctx context.Context, win layout.Window, record *CharacterRecord, name string) error {
	if err := s.click(ctx, win, s.Layout.TalentsTab, tabDelay); err != nil {
		return err
	}
	kinds := [talentCount]game.TalentKind{game.TalentAuto, game.TalentSkill, game.TalentBurst}
	var levels [talentCount]int
	var blue [talentCount]bool
	for i, kind := range kinds {
		position := i
		if kind == game.TalentBurst && hasAltSprint(name) {
			position++
		}
		rect := win.Rect(talentLevelBox(s.Layout, position))
		level, boosted, raw, err := s.readTalentLevel(ctx, rect)
		if err != nil {
			return err
		}
		record.Raw.Talents = append(record.Raw.Talents, raw)
		if level == 0 {
			record.issue("unreadable %s talent level %q", kind, raw)
		}
		levels[i], blue[i] = level, boosted
	}

	known, haveKnown := s.Catalog.BoostsFor(name)
	raised, note, issue := resolveBoosts(record.Character.Constellation, blue, known, haveKnown)
	if note != "" {
		record.note("%s", note)
	}
	if issue != "" {
		record.issue("%s", issue)
	}
	for i, kind := range kinds {
		if raised[i] && levels[i] > 0 {
			levels[i] -= 3
			record.Boosted = append(record.Boosted, kind)
		}
	}
	record.Character.Talent = game.Talents{Auto: levels[0], Skill: levels[1], Burst: levels[2]}
	return nil
}

func (s CharacterScanner) readTalentLevel(ctx context.Context, rect image.Rectangle) (level int, boosted bool, raw string, err error) {
	for attempt := range readAttempts {
		r, err := s.reader(ctx)
		if err != nil {
			return 0, false, raw, err
		}
		raw, err = r.readRect(rect, ocr.ModeLine, levelAllowed)
		if err != nil {
			return 0, false, raw, err
		}
		if level, ok := parseTalentLevel(raw); ok {
			return level, talentBoosted(r.frame, rect), raw, nil
		}
		if attempt < readAttempts-1 {
			if err := s.Pace.sleep(ctx, rereadDelay); err != nil {
				return 0, false, raw, err
			}
		}
	}
	return 0, false, raw, nil
}

func (s CharacterScanner) closeOverlay(ctx context.Context) error {
	if err := s.Game.PressEscape(); err != nil {
		return err
	}
	return s.Pace.sleep(ctx, overlayDelay)
}

func (s CharacterScanner) click(ctx context.Context, win layout.Window, p layout.Point, delay time.Duration) error {
	at := win.Point(p)
	if err := s.Game.Click(at.X, at.Y); err != nil {
		return err
	}
	return s.Pace.sleep(ctx, delay)
}

func (s CharacterScanner) reader(ctx context.Context) (reader, error) {
	frame, err := s.Game.Capture()
	if err != nil {
		return reader{}, err
	}
	return newReader(ctx, s.OCR, frame)
}
