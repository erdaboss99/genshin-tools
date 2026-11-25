package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/erdaboss99/genshin-tools/internal/export"
	"github.com/erdaboss99/genshin-tools/internal/game"
	"github.com/erdaboss99/genshin-tools/internal/layout"
	"github.com/erdaboss99/genshin-tools/internal/scan"
)

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(t.Context(), "test", args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		args []string
		code int
	}{
		{nil, 2},
		{[]string{"nonsense"}, 2},
		{[]string{"version"}, 0},
		{[]string{"help"}, 0},
		{[]string{"export"}, 2},
		{[]string{"export", "csv"}, 2},
		{[]string{"export", "good", "-h"}, 0},
		{[]string{"export", "good", "--no-such-flag"}, 2},
		{[]string{"export", "good", "stray"}, 2},
		{[]string{"export", "good", "-i", "/does/not/exist.json"}, 1},
		{[]string{"calibrate", "--screen", "bogus"}, 2},
		{[]string{"weapons", "-h"}, 0},
		{[]string{"weapons", "--no-such-flag"}, 2},
		{[]string{"characters", "-h"}, 0},
		{[]string{"characters", "--no-such-flag"}, 2},
		{[]string{"extract"}, 2},
		{[]string{"extract", "csv"}, 2},
		{[]string{"extract", "paimon", "-h"}, 0},
		{[]string{"extract", "paimon", "-i", "/does/not/exist.json"}, 1},
	}
	for _, tt := range tests {
		if code, _, _ := run(t, tt.args...); code != tt.code {
			t.Errorf("Run(%v) = %d, want %d", tt.args, code, tt.code)
		}
	}
}

func TestExportGOODFromReport(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "artifacts.json")
	output := filepath.Join(dir, "good.json")

	valid := game.Artifact{
		SetKey: "HeartOfDepth", Slot: game.SlotFlower, Rarity: 5, Level: 20, MainStat: game.StatHP,
		Substats: []game.Substat{
			{Key: game.StatATK, Value: 19}, {Key: game.StatDEF, Value: 23},
			{Key: game.StatCritRate, Value: 3.9}, {Key: game.StatCritDMG, Value: 7.8},
		},
	}
	records := []scan.ArtifactRecord{
		{Index: 0, Artifact: valid},
		{Index: 1, Artifact: valid, Issues: []string{"substat has an implausible value"}},
		{Index: 2, Artifact: game.Artifact{Slot: game.SlotFlower}},
	}
	if err := scan.WriteJSON(input, scan.NewReport(records, false)); err != nil {
		t.Fatal(err)
	}

	count := func(extra ...string) (int, string) {
		t.Helper()
		code, stdout, stderr := run(t, append([]string{"export", "good", "-i", input, "-o", output}, extra...)...)
		if code != 0 {
			t.Fatalf("export failed: %s", stderr)
		}
		raw, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var doc export.GOOD
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return len(doc.Artifacts), stdout
	}

	n, stdout := count()
	if n != 1 {
		t.Errorf("default export wrote %d artifacts, want 1", n)
	}
	if !strings.Contains(stdout, "stopped early") {
		t.Error("no warning for an incomplete report")
	}
	if n, _ := count("--include-issues"); n != 2 {
		t.Errorf("--include-issues wrote %d artifacts, want 2", n)
	}
}

func TestExportAchievementsMerges(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "achievements.json")
	merge := filepath.Join(dir, "site.json")
	output := filepath.Join(dir, "paimon.json")

	records := []scan.AchievementRecord{
		{Name: "Tiered", Category: 3, IDs: []game.AchievementID{10, 11, 12}, Stars: 2, Verified: true},
		{Name: "None", Category: 3, IDs: []game.AchievementID{20}},
		{Name: "Over", Category: 4, IDs: []game.AchievementID{30}, Stars: 5},
	}
	if err := scan.WriteJSON(input, scan.NewReport(records, true)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(merge, []byte(`{"server":"Europe"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := run(t, "export", "paimon", "-i", input, "-o", output, "--merge", merge)
	if code != 0 {
		t.Fatalf("export failed: %s", stderr)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Server      string                     `json:"server"`
		Achievement map[string]map[string]bool `json:"achievement"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Server != "Europe" {
		t.Error("merge lost existing data")
	}
	want := map[string]map[string]bool{"3": {"10": true, "11": true}, "4": {"30": true}}
	if got, _ := json.Marshal(doc.Achievement); string(got) != mustJSON(t, want) {
		t.Errorf("achievement = %s", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPrintLayoutRoundTrips(t *testing.T) {
	code, stdout, stderr := run(t, "calibrate", "--print-layout")
	if code != 0 {
		t.Fatalf("calibrate failed: %s", stderr)
	}
	path := filepath.Join(t.TempDir(), "layout.json")
	if err := os.WriteFile(path, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := layout.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != layout.Default() {
		t.Error("printed layout does not round-trip")
	}
}

// Setting these makes the test binary act as a fake tesseract.exe. They pass through
// to the child process along with the rest of the environment.
const (
	fakeGoodCallsEnv = "GENSIN_FAKE_TESSERACT_GOOD_CALLS"
	fakeCountFileEnv = "GENSIN_FAKE_TESSERACT_COUNT_FILE"
)

func TestMain(m *testing.M) {
	if goodCalls := os.Getenv(fakeGoodCallsEnv); goodCalls != "" {
		os.Exit(runFakeTesseract(goodCalls, os.Getenv(fakeCountFileEnv)))
	}
	os.Exit(m.Run())
}

// runFakeTesseract answers goodCalls OCR requests and then fails, as a crashed
// install or a broken language file would.
func runFakeTesseract(goodCalls, countFile string) int {
	if slices.Contains(os.Args[1:], "--list-langs") {
		fmt.Println("eng")
		return 0
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	limit, _ := strconv.Atoi(goodCalls)
	raw, _ := os.ReadFile(countFile)
	calls, _ := strconv.Atoi(string(raw))
	calls++
	if err := os.WriteFile(countFile, []byte(strconv.Itoa(calls)), 0o600); err != nil {
		return 1
	}
	if calls > limit {
		fmt.Fprintln(os.Stderr, "tesseract crashed")
		return 1
	}
	fmt.Println("text")
	return 0
}

// failingTesseract returns an executable that answers goodCalls OCR requests and then fails.
func failingTesseract(t *testing.T, goodCalls int) string {
	t.Helper()
	t.Setenv(fakeGoodCallsEnv, strconv.Itoa(goodCalls))
	t.Setenv(fakeCountFileEnv, filepath.Join(t.TempDir(), "count"))
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// artifactFrames copies test screenshots to a new directory.
func artifactFrames(t *testing.T, count int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "scan", "testdata", "artifact_circlet_1280.png"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for i := range count {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("frame-%d.png", i)), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFailedScanSavesPartialResults(t *testing.T) {
	frames := artifactFrames(t, 3)
	out := filepath.Join(t.TempDir(), "artifacts.json")

	// Enough for the first frame's reads, then a failure partway through the second.
	code, stdout, stderr := run(t, "artifacts", "--from-dir", frames, "--workers", "1", "--min-rarity", "1",
		"--tesseract", failingTesseract(t, 8), "-o", out)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "tesseract crashed") || !strings.Contains(stderr, "partial results saved") {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stdout, "Scanned 1 artifacts") {
		t.Errorf("stdout = %q", stdout)
	}
	report, err := scan.ReadReport[scan.ArtifactRecord](out)
	if err != nil {
		t.Fatalf("no report was saved: %v", err)
	}
	if report.Complete || len(report.Items) != 1 {
		t.Errorf("report: complete=%v, %d items; want incomplete with 1 item", report.Complete, len(report.Items))
	}
}

func TestFailedScanKeepsEarlierReportWhenNothingWasRead(t *testing.T) {
	frames := artifactFrames(t, 1)
	out := filepath.Join(t.TempDir(), "artifacts.json")
	const earlier = "earlier report"
	if err := os.WriteFile(out, []byte(earlier), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, _ := run(t, "artifacts", "--from-dir", frames, "--tesseract", failingTesseract(t, 0), "-o", out)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != earlier {
		t.Errorf("earlier report was replaced: %q", raw)
	}
}

func TestExportGOODIncludesWeapons(t *testing.T) {
	dir := t.TempDir()
	weapons := filepath.Join(dir, "weapons.json")
	output := filepath.Join(dir, "good.json")

	valid := game.Weapon{Key: "MistsplitterReforged", Level: 90, Ascension: 6, Refinement: 1}
	records := []scan.WeaponRecord{
		{Index: 0, Weapon: valid},
		{Index: 1, Weapon: valid, Issues: []string{"unreadable refinement"}},
		{Index: 2, Weapon: game.Weapon{Level: 1}},
	}
	if err := scan.WriteJSON(weapons, scan.NewReport(records, true)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run(t, "export", "good", "--weapons", weapons, "-i", filepath.Join(dir, "missing.json"), "-o", output)
	if code != 1 {
		t.Fatalf("an explicit artifact report that is missing must fail, got %d: %s", code, stderr)
	}
	// Without -i the missing artifact report is just skipped.
	code, stdout, stderr = run(t, "export", "good", "--weapons", weapons, "-o", output)
	if code != 0 {
		t.Fatalf("export failed: %s", stderr)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var doc export.GOOD
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Weapons) != 1 || doc.Weapons[0] != valid || len(doc.Artifacts) != 0 {
		t.Errorf("weapons = %+v, artifacts = %d", doc.Weapons, len(doc.Artifacts))
	}
	if !strings.Contains(stdout, "1 weapons") {
		t.Errorf("summary = %q", stdout)
	}
}

func TestExportGOODIncludesCharacters(t *testing.T) {
	dir := t.TempDir()
	characters := filepath.Join(dir, "characters.json")
	output := filepath.Join(dir, "good.json")

	valid := game.Character{Key: "HuTao", Level: 90, Ascension: 6, Constellation: 1, Talent: game.Talents{Auto: 10, Skill: 10, Burst: 10}}
	records := []scan.CharacterRecord{
		{Index: 0, Character: valid},
		{Index: 1, Character: valid, Issues: []string{"no constellation data"}},
		{Index: 2, Character: game.Character{Level: 1}},
	}
	if err := scan.WriteJSON(characters, scan.NewReport(records, true)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "export", "good", "--characters", characters, "-o", output)
	if code != 0 {
		t.Fatalf("export failed: %s", stderr)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var doc export.GOOD
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Characters) != 1 || doc.Characters[0] != valid {
		t.Errorf("characters = %+v", doc.Characters)
	}
	if !strings.Contains(stdout, "1 characters") {
		t.Errorf("summary = %q", stdout)
	}
}

func TestExtractPaimonRoundTrips(t *testing.T) {
	dir := t.TempDir()
	paimon := filepath.Join(dir, "paimon.json")
	report := filepath.Join(dir, "achievements.json")
	merged := filepath.Join(dir, "merged.json")

	// 80127 is the first stage of a three-stage achievement, 81000 a single-stage one,
	// and 999999 isn't in the catalog. The false entry must be ignored.
	source := `{"achievement":{"0":{"80127":true,"81000":true,"80128":false},"9":{"999999":true}},"wish-counter":{"pity":7}}`
	if err := os.WriteFile(paimon, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "extract", "paimon", "-i", paimon, "-o", report)
	if code != 0 {
		t.Fatalf("extract failed: %s", stderr)
	}
	if !strings.Contains(stdout, "3 completed") || !strings.Contains(stdout, "1 stages are not in the name list") {
		t.Errorf("summary = %q", stdout)
	}

	records, err := scan.ReadReport[scan.AchievementRecord](report)
	if err != nil {
		t.Fatalf("the report must be a valid scan report: %v", err)
	}
	if got := len(scan.CompletedAchievements(records.Items)); got != 3 {
		t.Errorf("report holds %d completed stages, want 3", got)
	}

	if code, _, stderr := run(t, "export", "paimon", "-i", report, "--merge", paimon, "-o", merged); code != 0 {
		t.Fatalf("export failed: %s", stderr)
	}
	raw, err := os.ReadFile(merged)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Achievement map[string]map[string]bool `json:"achievement"`
		Wish        map[string]int             `json:"wish-counter"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Achievement["0"]["80127"] || !doc.Achievement["0"]["81000"] || !doc.Achievement["9"]["999999"] || doc.Achievement["0"]["80128"] {
		t.Errorf("achievements did not survive the round trip: %v", doc.Achievement)
	}
	if doc.Wish["pity"] != 7 {
		t.Error("unrelated Paimon.moe data was lost")
	}
}
