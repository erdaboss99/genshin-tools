package scan

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/erdaboss99/genshin-tools/internal/game"
	"github.com/erdaboss99/genshin-tools/internal/imaging"
)

const (
	// progressInterval is how many items pass between progress log lines.
	progressInterval = 25

	savedFrameFormat = "frame-%05d.png"
)

// ScanOptions configures ScanArtifacts.
type ScanOptions struct {
	Workers int
	// SaveFramesDir, when set, stores each frame for later offline scans.
	SaveFramesDir string
	// MinRarity leaves out records below this rarity. Records with unknown rarity are
	// kept, because discarding them would hide scan failures.
	MinRarity int
	Log       *slog.Logger
}

// ScanArtifacts analyzes frames from source using a worker pool, returning records
// in frame order. Capture is much faster than OCR, so the queue is bounded to keep
// memory flat. Records finished before a failure are still returned with the error.
func ScanArtifacts(ctx context.Context, source FrameSource, parser ArtifactParser, opts ScanOptions) ([]ArtifactRecord, error) {
	return scanFrames(ctx, source, opts, "artifacts", frameHandler[ArtifactRecord]{
		parse: parser.Parse,
		annotate: func(r *ArtifactRecord, frame Frame, source string) {
			r.Index, r.Source = frame.Index, source
			if frame.Duplicate {
				r.issue(duplicateFrameIssue)
			}
		},
		index:  func(r ArtifactRecord) int { return r.Index },
		rarity: func(r ArtifactRecord) game.Rarity { return r.Artifact.Rarity },
	})
}

// ScanWeapons analyzes weapon frames like ScanArtifacts analyzes artifact ones.
func ScanWeapons(ctx context.Context, source FrameSource, parser WeaponParser, opts ScanOptions) ([]WeaponRecord, error) {
	return scanFrames(ctx, source, opts, "weapons", frameHandler[WeaponRecord]{
		parse: parser.Parse,
		annotate: func(r *WeaponRecord, frame Frame, source string) {
			r.Index, r.Source = frame.Index, source
			// Unlike artifacts, weapons come in stacks of identical copies, which
			// look the same as a missed click, so a repeat is only noted.
			if frame.Duplicate {
				r.Notes = append(r.Notes, RepeatNote)
			}
		},
		index:  func(r WeaponRecord) int { return r.Index },
		rarity: func(r WeaponRecord) game.Rarity { return r.Rarity },
	})
}

// RepeatNote marks a weapon whose panel was identical to the previous one's.
const RepeatNote = "identical to the previous weapon: another copy, or a click that didn't register"

const duplicateFrameIssue = "identical to the previous frame: the click may not have registered"

// frameHandler is what scanFrames needs to know about one kind of record.
type frameHandler[R any] struct {
	// parse reads a record from a frame. skipped is true for frames that show
	// something else, such as an upgrade material.
	parse func(ctx context.Context, frame *image.RGBA) (record R, skipped bool, err error)
	// annotate stamps the frame's position and source onto the record.
	annotate func(record *R, frame Frame, source string)
	// index returns the record's frame position, for ordering.
	index func(R) int
	// rarity returns the record's rarity, zero when it is unknown.
	rarity func(R) game.Rarity
}

func scanFrames[R any](ctx context.Context, source FrameSource, opts ScanOptions, noun string, h frameHandler[R]) ([]R, error) {
	workers := max(opts.Workers, 1)
	if opts.SaveFramesDir != "" {
		if err := os.MkdirAll(opts.SaveFramesDir, 0o755); err != nil {
			return nil, err
		}
	}

	workCtx, cancelWork := context.WithCancelCause(ctx)
	defer cancelWork(nil)
	queue := make(chan Frame, workers)

	var (
		mu      sync.Mutex
		records []R
		wg      sync.WaitGroup
	)
	for range workers {
		wg.Go(func() {
			for frame := range queue {
				record, skipped, err := analyzeFrame(workCtx, frame, h, opts)
				if err != nil {
					cancelWork(err)
					continue
				}
				if skipped {
					continue
				}
				mu.Lock()
				records = append(records, record)
				done := len(records)
				mu.Unlock()
				if opts.Log != nil && done%progressInterval == 0 {
					opts.Log.Info("progress", noun, done)
				}
			}
		})
	}

	sourceErr := source(workCtx, func(frame Frame) error {
		select {
		case queue <- frame:
			return nil
		case <-workCtx.Done():
			return context.Cause(workCtx)
		}
	})
	close(queue)
	wg.Wait()

	slices.SortFunc(records, func(a, b R) int { return h.index(a) - h.index(b) })
	// A worker's failure is the root cause, and the source's error is then only
	// its echo. Otherwise the source's error, such as lost focus, is the story.
	if cause := context.Cause(workCtx); cause != nil && ctx.Err() == nil {
		return records, cause
	}
	return records, sourceErr
}

// analyzeFrame reads one frame. skipped is true for frames that show something else
// and for records below the minimum rarity.
func analyzeFrame[R any](ctx context.Context, frame Frame, h frameHandler[R], opts ScanOptions) (record R, skipped bool, err error) {
	var zero R
	source := frame.Source
	if opts.SaveFramesDir != "" {
		source = filepath.Join(opts.SaveFramesDir, fmt.Sprintf(savedFrameFormat, frame.Index))
		if err := imaging.SavePNG(source, frame.Image); err != nil {
			return zero, false, fmt.Errorf("save frame: %w", err)
		}
	}
	record, skipped, err = h.parse(ctx, frame.Image)
	if err != nil || skipped {
		return zero, skipped, err
	}
	h.annotate(&record, frame, source)
	if rarity := h.rarity(record); rarity != 0 && int(rarity) < opts.MinRarity {
		return zero, true, nil
	}
	return record, false, nil
}
