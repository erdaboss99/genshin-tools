package cli

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"time"

	"github.com/erdaboss99/genshin-tools/internal/scan"
)

// weaponOptions holds the weapons command's flags.
type weaponOptions struct {
	engine     engineFlags
	out        string
	fromDir    string
	saveFrames string
	workers    int
	limit      int
	count      int
	minRarity  int
	traveler   string
	pace       float64
	countdown  time.Duration
}

func (o *weaponOptions) register(flags *flag.FlagSet) {
	o.engine.register(flags)
	flags.StringVar(&o.out, "o", "weapons.json", "output report file")
	flags.StringVar(&o.fromDir, "from-dir", "", "scan the screenshots in this directory instead of the live game")
	flags.StringVar(&o.saveFrames, "save-frames", "", "also save each captured frame to this directory, for re-scanning with --from-dir")
	flags.IntVar(&o.workers, "workers", defaultWorkers(), "number of parallel OCR processes")
	flags.IntVar(&o.limit, "limit", 0, "stop after this many items (0 scans everything)")
	flags.IntVar(&o.count, "count", 0, "number of items in the inventory (0 reads it from the screen)")
	flags.IntVar(&o.minRarity, "min-rarity", 3, "leave weapons below this rarity out of the report")
	flags.StringVar(&o.traveler, "traveler-name", "", "your in-game name, shown instead of \"Traveler\" on equipped weapons")
	flags.Float64Var(&o.pace, "pace", 1, "multiplier for delays between actions; raise it if clicks get missed")
	flags.DurationVar(&o.countdown, "countdown", defaultCountdown, "delay before a live scan starts")
}

// keepWeapons filters weapon records in place by minimum rarity. Records with
// unknown rarity are kept, because discarding them would hide scan failures.
func keepWeapons(records []scan.WeaponRecord, minRarity int) []scan.WeaponRecord {
	return slices.DeleteFunc(records, func(r scan.WeaponRecord) bool {
		return r.Rarity != 0 && int(r.Rarity) < minRarity
	})
}

func (a *app) runWeapons(ctx context.Context, args []string) error {
	var opts weaponOptions
	flags := a.newFlagSet("weapons")
	opts.register(flags)
	if err := parseFlags(flags, args); err != nil {
		return err
	}

	engine, err := opts.engine.load(ctx)
	if err != nil {
		return err
	}
	parser := scan.WeaponParser{
		Layout:       engine.layout.Weapons,
		OCR:          engine.ocr,
		Catalog:      engine.catalog,
		TravelerName: opts.traveler,
	}
	source, err := a.weaponSource(ctx, opts, engine, parser)
	if err != nil {
		return err
	}

	records, scanErr := scan.ScanWeapons(ctx, source, parser, scan.ScanOptions{
		Workers:       opts.workers,
		SaveFramesDir: opts.saveFrames,
		Log:           a.log,
	})
	// A failure with nothing to show must not replace an earlier report.
	if scanErr != nil && len(records) == 0 {
		return scanErr
	}
	records = keepWeapons(records, opts.minRarity)
	if err := scan.WriteJSON(opts.out, scan.NewReport(records, scanErr == nil)); err != nil {
		return err
	}
	a.printWeaponSummary(records, opts.out)
	if scanErr != nil {
		return fmt.Errorf("scan stopped early, partial results saved: %w", scanErr)
	}
	return nil
}

// weaponSource picks saved screenshots or the live game.
func (a *app) weaponSource(ctx context.Context, opts weaponOptions, engine engine, parser scan.WeaponParser) (scan.FrameSource, error) {
	if opts.fromDir != "" {
		return scan.DirFrames(opts.fromDir), nil
	}
	game, err := a.attachForInput(ctx, opts.countdown)
	if err != nil {
		return nil, err
	}
	return scan.InventoryWalker{
		Game:       game,
		Layout:     engine.layout.Weapons.Browse(),
		OCR:        engine.ocr,
		IsMaterial: parser.IsUnrelated,
		Pace:       scan.Pace(opts.pace),
		Limit:      opts.limit,
		Count:      opts.count,
		Tab:        "Weapons",
		Log:        a.log,
	}.Walk, nil
}

func (a *app) printWeaponSummary(records []scan.WeaponRecord, path string) {
	withIssues, repeats := 0, 0
	for _, r := range records {
		if len(r.Issues) > 0 {
			withIssues++
		}
		if len(r.Notes) > 0 {
			repeats++
		}
	}
	fmt.Fprintf(a.stdout, "Scanned %d weapons: %d exportable, %d with issues.\n", len(records), len(records)-withIssues, withIssues)
	if repeats > 0 {
		fmt.Fprintf(a.stdout, "%d repeat the weapon before them, which is normal for stacks of the same weapon.\n", repeats)
	}
	fmt.Fprintf(a.stdout, "Report written to %s\n", path)
	if withIssues > 0 {
		fmt.Fprintln(a.stdout, `Check the "issues" fields in the report. If many fail the same way, run "genshin-tools calibrate --screen weapons".`)
	}
}
