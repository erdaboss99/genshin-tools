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

	"github.com/erdaboss99/gensin-tools/internal/imaging"
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
	Log           *slog.Logger
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
		index: func(r ArtifactRecord) int { return r.Index },
	})
}

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
				record, skipped, err := analyzeFrame(workCtx, frame, h, opts.SaveFramesDir)
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

func analyzeFrame[R any](ctx context.Context, frame Frame, h frameHandler[R], saveDir string) (R, bool, error) {
	var zero R
	source := frame.Source
	if saveDir != "" {
		source = filepath.Join(saveDir, fmt.Sprintf(savedFrameFormat, frame.Index))
		if err := imaging.SavePNG(source, frame.Image); err != nil {
			return zero, false, fmt.Errorf("save frame: %w", err)
		}
	}
	record, skipped, err := h.parse(ctx, frame.Image)
	if err != nil {
		return zero, false, err
	}
	h.annotate(&record, frame, source)
	return record, skipped, nil
}
