package export

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/erdaboss99/gensin-tools/internal/game"
)

const (
	paimonAchievementKey = "achievement"
	seelieAchievementKey = "achievements"
)

// paimonAchievements is Paimon.moe's completed list: category ID to achievement ID.
type paimonAchievements map[string]map[string]bool

// seelieState is one achievement's state in a Seelie.me export.
type seelieState struct {
	Done bool `json:"done"`
}

// seelieAchievements is Seelie.me's completed list, by achievement ID.
type seelieAchievements map[string]seelieState

// exportDocument is a site's export, whose unrecognized fields must survive a merge.
type exportDocument map[string]json.RawMessage

// PaimonMoe merges achievements into a Paimon.moe export. existing may be nil, or
// the site's own export, whose other fields are preserved untouched.
func PaimonMoe(completed []game.CompletedAchievement, existing []byte) ([]byte, error) {
	categories := make(paimonAchievements)
	for _, c := range completed {
		key := strconv.Itoa(int(c.Category))
		if categories[key] == nil {
			categories[key] = make(map[string]bool)
		}
		categories[key][strconv.Itoa(int(c.ID))] = true
	}
	return mergeInto(existing, paimonAchievementKey, categories)
}

// SeelieMe merges achievements into a Seelie.me export. existing may be nil, or the
// site's own export, whose other fields are preserved untouched.
func SeelieMe(completed []game.CompletedAchievement, existing []byte) ([]byte, error) {
	achievements := make(seelieAchievements, len(completed))
	for _, c := range completed {
		achievements[strconv.Itoa(int(c.ID))] = seelieState{Done: true}
	}
	return mergeInto(existing, seelieAchievementKey, achievements)
}

func mergeInto[T any](existing []byte, key string, value T) ([]byte, error) {
	doc := make(exportDocument)
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("parse existing export: %w", err)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	doc[key] = raw
	return json.MarshalIndent(doc, "", "  ")
}

// ReadPaimonMoe extracts the completed achievements from a Paimon.moe export. Entries
// marked false are skipped. The result is sorted by category, then ID.
func ReadPaimonMoe(raw []byte) ([]game.CompletedAchievement, error) {
	var doc struct {
		Achievement paimonAchievements `json:"achievement"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse Paimon.moe export: %w", err)
	}
	if doc.Achievement == nil {
		return nil, errors.New("no \"achievement\" section in this Paimon.moe export")
	}
	var completed []game.CompletedAchievement
	for categoryKey, ids := range doc.Achievement {
		category, err := strconv.Atoi(categoryKey)
		if err != nil {
			return nil, fmt.Errorf("achievement category %q is not a number", categoryKey)
		}
		for idKey, done := range ids {
			id, err := strconv.Atoi(idKey)
			if err != nil {
				return nil, fmt.Errorf("achievement ID %q is not a number", idKey)
			}
			if done {
				completed = append(completed, game.CompletedAchievement{ID: game.AchievementID(id), Category: game.CategoryID(category)})
			}
		}
	}
	slices.SortFunc(completed, func(a, b game.CompletedAchievement) int {
		return cmp.Or(cmp.Compare(a.Category, b.Category), cmp.Compare(a.ID, b.ID))
	})
	return completed, nil
}
