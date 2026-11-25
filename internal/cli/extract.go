package cli

import (
	"fmt"
	"os"
	"slices"

	"github.com/erdaboss99/genshin-tools/internal/catalog"
	"github.com/erdaboss99/genshin-tools/internal/export"
	"github.com/erdaboss99/genshin-tools/internal/game"
	"github.com/erdaboss99/genshin-tools/internal/scan"
)

// paimonSource marks records that came from a Paimon.moe export rather than a scan.
const paimonSource = "paimon.moe"

func (a *app) runExtract(args []string) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(a.stdout, extractUsage)
		return nil
	}
	if len(args) == 0 || args[0] != "paimon" {
		fmt.Fprint(a.stderr, extractUsage)
		return errUsage
	}
	var in, out, dataDir string
	var list bool
	flags := a.newFlagSet("extract paimon")
	flags.StringVar(&in, "i", "paimon.json", "Paimon.moe export to read")
	flags.StringVar(&out, "o", "achievements.json", "achievement report to write")
	flags.StringVar(&dataDir, "data-dir", "", "directory with downloaded names (default: the user cache)")
	flags.BoolVar(&list, "list", false, "print the completed achievements instead of writing a report")
	if err := parseFlags(flags, args[1:]); err != nil {
		return err
	}

	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	completed, err := export.ReadPaimonMoe(raw)
	if err != nil {
		return err
	}
	if dataDir == "" {
		if dataDir, err = catalog.DefaultDir(); err != nil {
			dataDir = "" // No cache directory is available, so use the embedded snapshot.
		}
	}
	cat, err := catalog.Load(dataDir)
	if err != nil {
		return err
	}
	records, unknown := paimonRecords(cat, completed)

	if list {
		for _, r := range records {
			fmt.Fprintf(a.stdout, "%d/%d\t%s\n", r.Stars, len(r.IDs), r.Name)
		}
		fmt.Fprintf(a.stdout, "%d completed achievement stages.\n", len(completed))
		return nil
	}
	if err := scan.WriteJSON(out, scan.NewReport(records, true)); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Wrote %d completed achievement stages to %s.\n", len(completed), out)
	if unknown > 0 {
		fmt.Fprintf(a.stdout, "%d stages are not in the name list and were kept by ID only; run update-data to name them.\n", unknown)
	}
	return nil
}

// paimonRecords turns completed stages into report records: one per catalog
// achievement with at least one stage done, plus one per stage the catalog lacks.
// Stages complete in order, so Stars counts the leading stages that are done.
func paimonRecords(cat *catalog.Catalog, completed []game.CompletedAchievement) (records []scan.AchievementRecord, unknown int) {
	done := make(map[game.AchievementID]bool, len(completed))
	for _, c := range completed {
		done[c.ID] = true
	}
	known := make(map[game.AchievementID]bool)
	for _, entry := range cat.Achievements {
		stars := 0
		for _, id := range entry.IDs {
			known[id] = true
			if stars == slices.Index(entry.IDs, id) && done[id] {
				stars++
			}
		}
		if stars == 0 {
			continue
		}
		records = append(records, scan.AchievementRecord{
			Name: entry.Name, Category: entry.Category, IDs: entry.IDs,
			Stars: stars, Verified: true, Source: paimonSource,
		})
	}
	for _, c := range completed {
		if !known[c.ID] {
			unknown++
			records = append(records, scan.AchievementRecord{
				Name: fmt.Sprintf("Unknown achievement %d", c.ID), Category: c.Category,
				IDs: []game.AchievementID{c.ID}, Stars: 1, Verified: true, Source: paimonSource,
			})
		}
	}
	return records, unknown
}
