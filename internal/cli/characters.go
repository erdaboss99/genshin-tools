package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/erdaboss99/gensin-tools/internal/scan"
)

// characterOptions holds the characters command's flags.
type characterOptions struct {
	engine     engineFlags
	out        string
	limit      int
	saveFrames string
	traveler   string
	pace       float64
	countdown  time.Duration
}

func (o *characterOptions) register(flags *flag.FlagSet) {
	o.engine.register(flags)
	flags.StringVar(&o.out, "o", "characters.json", "output report file")
	flags.IntVar(&o.limit, "limit", 0, "stop after this many characters (0 scans everything)")
	flags.StringVar(&o.saveFrames, "save-frames", "", "save a screenshot of each page read to this directory, to check the results against")
	flags.StringVar(&o.traveler, "traveler-name", "", "your in-game name, shown instead of \"Traveler\"")
	flags.Float64Var(&o.pace, "pace", 1, "multiplier for delays between actions; raise it if screens get missed")
	flags.DurationVar(&o.countdown, "countdown", defaultCountdown, "delay before the scan starts")
}

func (a *app) runCharacters(ctx context.Context, args []string) error {
	var opts characterOptions
	flags := a.newFlagSet("characters")
	opts.register(flags)
	if err := parseFlags(flags, args); err != nil {
		return err
	}

	engine, err := opts.engine.load(ctx)
	if err != nil {
		return err
	}
	game, err := a.attachForInput(ctx, opts.countdown)
	if err != nil {
		return err
	}
	scanner := scan.CharacterScanner{
		Game:          game,
		Layout:        engine.layout.Characters,
		OCR:           engine.ocr,
		Catalog:       engine.catalog,
		TravelerName:  opts.traveler,
		Pace:          scan.Pace(opts.pace),
		Limit:         opts.limit,
		SaveFramesDir: opts.saveFrames,
		Log:           a.log,
	}
	records, scanErr := scanner.Scan(ctx)
	// A failure with nothing to show must not replace an earlier report.
	if scanErr != nil && len(records) == 0 {
		return scanErr
	}
	if err := scan.WriteJSON(opts.out, scan.NewReport(records, scanErr == nil)); err != nil {
		return err
	}
	a.printCharacterSummary(records, opts.out)
	if scanErr != nil {
		return fmt.Errorf("scan stopped early, partial results saved: %w", scanErr)
	}
	return nil
}

func (a *app) printCharacterSummary(records []scan.CharacterRecord, path string) {
	withIssues := 0
	for _, r := range records {
		if len(r.Issues) > 0 {
			withIssues++
		}
	}
	fmt.Fprintf(a.stdout, "Scanned %d characters: %d exportable, %d with issues.\n", len(records), len(records)-withIssues, withIssues)
	fmt.Fprintf(a.stdout, "Report written to %s\n", path)
	if withIssues > 0 {
		fmt.Fprintln(a.stdout, `Check the "issues" fields in the report. If many fail the same way, run "gensin-scan calibrate --screen characters".`)
	}
}
