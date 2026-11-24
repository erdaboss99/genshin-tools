package cli

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/erdaboss99/gensin-tools/internal/export"
	"github.com/erdaboss99/gensin-tools/internal/game"
	"github.com/erdaboss99/gensin-tools/internal/scan"
)

func (a *app) runExport(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(a.stderr, "usage: gensin-scan export <good|paimon|seelie> [flags]")
		return errUsage
	}
	format, rest := args[0], args[1:]
	switch format {
	case "good":
		return a.exportGOOD(rest)
	case "paimon":
		return a.exportAchievements("paimon", "paimon.json", export.PaimonMoe, rest)
	case "seelie":
		return a.exportAchievements("seelie", "seelie.json", export.SeelieMe, rest)
	}
	fmt.Fprintf(a.stderr, "unknown export format %q (want good, paimon or seelie)\n", format)
	return errUsage
}

// goodExportOptions holds the export good command's flags.
type goodExportOptions struct {
	in            string
	weapons       string
	characters    string
	materials     string
	out           string
	includeIssues bool
}

func (o *goodExportOptions) register(flags *flag.FlagSet) {
	flags.StringVar(&o.in, "i", "artifacts.json", "artifact report to read")
	flags.StringVar(&o.weapons, "weapons", "weapons.json", "weapon report to read")
	flags.StringVar(&o.characters, "characters", "characters.json", "character report to read")
	flags.StringVar(&o.materials, "materials", "materials.json", "material report to read")
	flags.StringVar(&o.out, "o", "good.json", "GOOD file to write")
	flags.BoolVar(&o.includeIssues, "include-issues", false, "keep items with scan issues, as long as they pass the game's rules; these may have wrong or missing values")
}

// readOptionalReport reads a report that may be absent. A report named on the command
// line must exist, while a default path that isn't there is skipped.
func readOptionalReport[T any](path string, explicit bool) (report scan.Report[T], found bool, err error) {
	report, err = scan.ReadReport[T](path)
	if errors.Is(err, fs.ErrNotExist) && !explicit {
		return report, false, nil
	}
	return report, err == nil, err
}

func (a *app) exportGOOD(args []string) error {
	var opts goodExportOptions
	flags := a.newFlagSet("export good")
	opts.register(flags)
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	explicit := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	var input export.GOODInput
	var incomplete []string
	skipped := 0
	keep := func(issues []string) bool {
		if !opts.includeIssues && len(issues) > 0 {
			skipped++
			return false
		}
		return true
	}

	artifacts, ok, err := readOptionalReport[scan.ArtifactRecord](opts.in, explicit["i"])
	if err != nil {
		return err
	}
	if ok {
		for _, r := range artifacts.Items {
			if keep(r.Issues) {
				input.Artifacts = append(input.Artifacts, r.Artifact)
			}
		}
		if !artifacts.Complete {
			incomplete = append(incomplete, opts.in)
		}
	}
	weapons, hasWeapons, err := readOptionalReport[scan.WeaponRecord](opts.weapons, explicit["weapons"])
	if err != nil {
		return err
	}
	if hasWeapons {
		for _, r := range weapons.Items {
			if keep(r.Issues) {
				input.Weapons = append(input.Weapons, r.Weapon)
			}
		}
		if !weapons.Complete {
			incomplete = append(incomplete, opts.weapons)
		}
	}
	characters, hasCharacters, err := readOptionalReport[scan.CharacterRecord](opts.characters, explicit["characters"])
	if err != nil {
		return err
	}
	if hasCharacters {
		for _, r := range characters.Items {
			if keep(r.Issues) {
				input.Characters = append(input.Characters, r.Character)
			}
		}
		if !characters.Complete {
			incomplete = append(incomplete, opts.characters)
		}
	}
	materials, hasMaterials, err := readOptionalReport[scan.MaterialRecord](opts.materials, explicit["materials"])
	if err != nil {
		return err
	}
	if hasMaterials {
		input.Materials = make(map[string]int, len(materials.Items))
		for _, r := range materials.Items {
			input.Materials[r.Key] += r.Count
		}
		if !materials.Complete {
			incomplete = append(incomplete, opts.materials)
		}
	}
	if !ok && !hasWeapons && !hasCharacters && !hasMaterials {
		return fmt.Errorf("nothing to export: none of %s, %s, %s or %s exists", opts.in, opts.weapons, opts.characters, opts.materials)
	}

	doc, rejected := export.BuildGOOD(input)
	if err := scan.WriteJSON(opts.out, doc); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Wrote %d artifacts, %d weapons, %d characters and %d materials to %s (%d invalid and %d with issues were left out).\n",
		len(doc.Artifacts), len(doc.Weapons), len(doc.Characters), len(doc.Materials), opts.out, len(rejected), skipped)
	for _, path := range incomplete {
		fmt.Fprintf(a.stdout, "Warning: %s comes from a scan that stopped early.\n", path)
	}
	return nil
}

// achievementExporter merges completed achievements into an export document.
type achievementExporter func(completed []game.CompletedAchievement, existing []byte) ([]byte, error)

// achievementExportOptions holds the flags of the achievement export commands.
type achievementExportOptions struct {
	in    string
	out   string
	merge string
}

func (o *achievementExportOptions) register(flags *flag.FlagSet, defaultOut string) {
	flags.StringVar(&o.in, "i", "achievements.json", "achievement report to read")
	flags.StringVar(&o.out, "o", defaultOut, "file to write")
	flags.StringVar(&o.merge, "merge", "", "the site's own export, to keep everything except achievements")
}

func (a *app) exportAchievements(name, defaultOut string, exporter achievementExporter, args []string) error {
	var opts achievementExportOptions
	flags := a.newFlagSet("export " + name)
	opts.register(flags, defaultOut)
	if err := parseFlags(flags, args); err != nil {
		return err
	}

	report, err := scan.ReadReport[scan.AchievementRecord](opts.in)
	if err != nil {
		return err
	}
	var existing []byte
	if opts.merge != "" {
		if existing, err = os.ReadFile(opts.merge); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("--merge: %w", err)
			}
			return err
		}
	}

	completed := scan.CompletedAchievements(report.Items)
	raw, err := exporter(completed, existing)
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.out, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Wrote %d completed achievement stages to %s.\n", len(completed), opts.out)
	if !report.Complete {
		fmt.Fprintln(a.stdout, "Warning: this report comes from a scan that stopped early.")
	}
	return nil
}
