package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
)

const usage = `genshin-tools reads Genshin Impact artifacts, weapons, characters and
achievements using OCR, and exports them for other tools.

Usage:
  genshin-tools <command> [arguments] [flags]

Commands:
  scan          Scan the game, or check and calibrate the setup for scanning
  export        Convert scan reports for Genshin Optimizer, Paimon.moe or Seelie.me
  extract       Turn a tracking site's export into a scan report
  update-data   Download the latest artifact, character, weapon and achievement names
  version       Print the version

Run "genshin-tools scan", "genshin-tools export" or "genshin-tools extract" to list
their subcommands. Add -h to any command or subcommand for its flags.
`

const scanUsage = `Usage:
  genshin-tools scan <subcommand> [flags]

Scanning:
  artifacts     Scan artifacts from the live game or saved screenshots
  characters    Scan characters from the live game, with the character screen open
  weapons       Scan weapons from the live game or saved screenshots
  achievements  Scan achievements from the live game or saved screenshots

Setup:
  doctor        Check that everything needed for scanning is in place
  calibrate     Draw the scan regions on a screenshot and show what OCR reads
  capture       Save a screenshot of the game window

Run "genshin-tools scan <subcommand> -h" for a subcommand's flags.
`

const exportUsage = `Usage:
  genshin-tools export <format> [flags]

Formats:
  good    Convert the artifact, weapon and character scans to GOOD (Genshin Optimizer and others)
  paimon  Convert an achievement scan to Paimon.moe
  seelie  Convert an achievement scan to Seelie.me

Run "genshin-tools export <format> -h" for a format's flags.
`

const extractUsage = `Usage:
  genshin-tools extract <source> [flags]

Sources:
  paimon  Turn a Paimon.moe export into achievements.json

Run "genshin-tools extract <source> -h" for a source's flags.
`

// errUsage marks errors whose message has already been shown by the flag package.
var errUsage = errors.New("invalid usage")

// app holds what every command needs.
type app struct {
	version string
	stdout  io.Writer
	stderr  io.Writer
	log     *slog.Logger
}

// Run executes the command line and returns the process exit code.
func Run(ctx context.Context, version string, args []string, stdout, stderr io.Writer) int {
	a := &app{
		version: version,
		stdout:  stdout,
		stderr:  stderr,
		log:     slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
	if err := a.dispatch(ctx, args); err != nil {
		switch {
		case errors.Is(err, flag.ErrHelp):
			return 0
		case errors.Is(err, errUsage):
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func (a *app) dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usage)
		return errUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case "scan":
		return a.runScan(ctx, rest)
	case "export":
		return a.runExport(rest)
	case "extract":
		return a.runExtract(rest)
	case "update-data":
		return a.runUpdateData(ctx, rest)
	case "version":
		fmt.Fprintf(a.stdout, "genshin-tools %s\n", a.version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, usage)
		return nil
	}
	fmt.Fprintf(a.stderr, "unknown command %q\n\n%s", command, usage)
	return errUsage
}

// runScan dispatches the scan subcommands: the scans themselves and the commands that
// check and calibrate the setup for them.
func (a *app) runScan(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, scanUsage)
		return errUsage
	}
	subcommand, rest := args[0], args[1:]
	switch subcommand {
	case "artifacts":
		return a.runArtifacts(ctx, rest)
	case "characters":
		return a.runCharacters(ctx, rest)
	case "weapons":
		return a.runWeapons(ctx, rest)
	case "achievements":
		return a.runAchievements(ctx, rest)
	case "doctor":
		return a.runDoctor(ctx, rest)
	case "calibrate":
		return a.runCalibrate(ctx, rest)
	case "capture":
		return a.runCapture(ctx, rest)
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, scanUsage)
		return nil
	}
	fmt.Fprintf(a.stderr, "unknown scan subcommand %q\n\n%s", subcommand, scanUsage)
	return errUsage
}

// newFlagSet returns a flag set whose errors surface as errUsage.
func (a *app) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("genshin-tools "+name, flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flag.ErrHelp
		}
		return errUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(fs.Output(), "unexpected argument %q\n", fs.Arg(0))
		return errUsage
	}
	return nil
}
