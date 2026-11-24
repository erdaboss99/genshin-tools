package scan

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
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
	// talentMaxBase is the highest a talent can be raised, before constellation bonuses.
	talentMaxBase = 10
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
	// SaveFramesDir, when set, stores a screenshot of each page that was read, so the
	// results can be checked against what the screen showed.
	SaveFramesDir string
	Log           *slog.Logger
}

// characterShots are the pages read for one character, kept for saving.
type characterShots struct {
	attributes, constellation, talents *image.RGBA
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
		record, shots, err := s.scanCharacter(ctx, win, first)
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
		if err := s.saveShots(record, shots); err != nil {
			return s.finish(records), err
		}
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
func (s CharacterScanner) scanCharacter(ctx context.Context, win layout.Window, first game.CharacterKey) (CharacterRecord, *characterShots, error) {
	var record CharacterRecord
	shots := &characterShots{}
	if err := s.click(ctx, win, s.Layout.AttributesTab, tabDelay); err != nil {
		return record, shots, err
	}

	header, agreed, valid, err := s.readHeader(ctx, &record, shots)
	if err != nil {
		return record, shots, err
	}
	if !valid {
		record.issue("unreadable name %q (if this is the Traveler, pass --traveler-name with your in-game name)", record.Raw.Header)
		return record, shots, nil
	}
	if !agreed {
		record.issue("the name kept changing between reads, last %q", record.Raw.Header)
	}
	c := &record.Character
	c.Key, record.Element = header.Key, header.Element
	if c.Key == first {
		return record, shots, nil
	}

	if err := s.readLevel(ctx, &record); err != nil {
		return record, shots, err
	}
	if err := s.readConstellation(ctx, win, &record, shots); err != nil {
		return record, shots, err
	}
	if err := s.readTalents(ctx, win, &record, header.Name, shots); err != nil {
		return record, shots, err
	}

	if verr := c.Validate(); verr != nil {
		for line := range strings.SplitSeq(verr.Error(), "\n") {
			if !slices.Contains(record.Issues, line) {
				record.issue("%s", line)
			}
		}
	}
	return record, shots, nil
}

// readStable calls read until the same valid value has come up twice. A screen that
// is still fading in can give a wrong but plausible reading once, such as an extra
// digit, and it rarely gives the same wrong one twice. Reads that can't be parsed in
// between don't matter. When no value repeats it returns the last valid one with
// agreed false, and valid is false if there never was one.
func readStable[T comparable](ctx context.Context, pace Pace, read func() (T, bool, error)) (value T, agreed, valid bool, err error) {
	seen := make(map[T]int)
	for attempt := range readAttempts {
		v, ok, err := read()
		if err != nil {
			return value, false, valid, err
		}
		if ok {
			if seen[v]++; seen[v] >= 2 {
				return v, true, true, nil
			}
			value, valid = v, true
		}
		if attempt < readAttempts-1 {
			if err := pace.sleep(ctx, rereadDelay); err != nil {
				return value, false, valid, err
			}
		}
	}
	return value, false, valid, nil
}

func (s CharacterScanner) readHeader(ctx context.Context, record *CharacterRecord, shots *characterShots) (characterHeader, bool, bool, error) {
	return readStable(ctx, s.Pace, func() (characterHeader, bool, error) {
		r, err := s.reader(ctx)
		if err != nil {
			return characterHeader{}, false, err
		}
		shots.attributes = r.frame
		text, err := r.read(s.Layout.Header, ocr.ModeBlock, "")
		if err != nil {
			return characterHeader{}, false, err
		}
		record.Raw.Header = text
		header, ok := parseCharacterHeader(text, s.Catalog, s.TravelerName)
		return header, ok, nil
	})
}

// levelReading is a character's level and ascension as read from the level text.
type levelReading struct {
	level     game.Level
	ascension int
}

func (s CharacterScanner) readLevel(ctx context.Context, record *CharacterRecord) error {
	reading, agreed, valid, err := readStable(ctx, s.Pace, func() (levelReading, bool, error) {
		r, err := s.reader(ctx)
		if err != nil {
			return levelReading{}, false, err
		}
		text, err := r.read(s.Layout.Level, ocr.ModeLine, levelAllowed)
		if err != nil {
			return levelReading{}, false, err
		}
		record.Raw.Level = text
		level, ascension, ok := parseLevelCap(text)
		return levelReading{level, ascension}, ok, nil
	})
	if err != nil {
		return err
	}
	switch {
	case !valid:
		record.issue("unreadable level %q", record.Raw.Level)
	case !agreed:
		record.issue("the level kept changing between reads, last %q", record.Raw.Level)
		fallthrough
	default:
		record.Character.Level, record.Character.Ascension = reading.level, reading.ascension
	}
	return nil
}

// readConstellation opens each constellation in turn. One that isn't unlocked shows a
// white "Activate" button, so the count is how many come before the first of those.
func (s CharacterScanner) readConstellation(ctx context.Context, win layout.Window, record *CharacterRecord, shots *characterShots) error {
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
		shots.constellation = frame
		if constellationLocked(imaging.MeanColor(frame, win.Rect(s.Layout.Activate))) {
			break
		}
	}
	record.Character.Constellation = count
	return s.closeOverlay(ctx)
}

// readTalents reads the three talent levels from the talents page, which lists them
// all, and notes which are drawn in blue. Those are the ones constellations raised.
func (s CharacterScanner) readTalents(ctx context.Context, win layout.Window, record *CharacterRecord, name string, shots *characterShots) error {
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
		reading, raw, agreed, valid, err := s.readTalentLevel(ctx, rect, shots)
		if err != nil {
			return err
		}
		record.Raw.Talents = append(record.Raw.Talents, raw)
		switch {
		case !valid:
			record.issue("unreadable %s talent level %q", kind, raw)
		case !agreed:
			record.issue("the %s talent level kept changing between reads, last %q", kind, raw)
			fallthrough
		default:
			levels[i], blue[i] = reading.level, reading.boosted
		}
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
	for i, kind := range kinds {
		if levels[i] > talentMaxBase {
			record.issue("the %s talent level is %d, but %d is the most it can be without bonuses", kind, levels[i], talentMaxBase)
		}
	}
	return nil
}

// talentReading is a talent's level and whether it is drawn in blue.
type talentReading struct {
	level   int
	boosted bool
}

func (s CharacterScanner) readTalentLevel(ctx context.Context, rect image.Rectangle, shots *characterShots) (reading talentReading, raw string, agreed, valid bool, err error) {
	reading, agreed, valid, err = readStable(ctx, s.Pace, func() (talentReading, bool, error) {
		r, err := s.reader(ctx)
		if err != nil {
			return talentReading{}, false, err
		}
		shots.talents = r.frame
		// Blue text is hard to read on its darkest channel, where it has little
		// contrast, and reads cleanly on its green one. Everything else reads best
		// the other way round. Each is the fallback for the other.
		boosted := talentBoosted(r.frame, rect)
		channels := []imaging.Channel{imaging.Darkest, imaging.Green}
		if boosted {
			channels = []imaging.Channel{imaging.Green, imaging.Darkest}
		}
		for _, channel := range channels {
			text, err := r.readRectChannel(rect, ocr.ModeBlock, levelAllowed, imaging.OCRScale(r.win.Width), channel)
			if err != nil {
				return talentReading{}, false, err
			}
			raw = text
			if level, ok := parseTalentLevel(text); ok {
				return talentReading{level, boosted}, true, nil
			}
		}
		return talentReading{}, false, nil
	})
	return reading, raw, agreed, valid, err
}

// saveShots stores the pages read for a character, named by position and key.
func (s CharacterScanner) saveShots(record CharacterRecord, shots *characterShots) error {
	if s.SaveFramesDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.SaveFramesDir, 0o755); err != nil {
		return err
	}
	name := cmp.Or(string(record.Character.Key), "unknown")
	for page, frame := range map[string]*image.RGBA{
		"attributes": shots.attributes, "constellation": shots.constellation, "talents": shots.talents,
	} {
		if frame == nil {
			continue
		}
		path := filepath.Join(s.SaveFramesDir, fmt.Sprintf("%03d-%s-%s.png", record.Index, name, page))
		if err := imaging.SavePNG(path, frame); err != nil {
			return fmt.Errorf("save frame: %w", err)
		}
	}
	return nil
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
