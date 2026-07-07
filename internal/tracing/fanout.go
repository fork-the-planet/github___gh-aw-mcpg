package tracing

import (
	"context"
	"errors"
	"sync"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// fanoutExporter is a SpanExporter that fans out span export to multiple
// underlying exporters. All exporters are attempted even when earlier ones
// fail (partial-failure tolerance), and collected errors are joined before
// returning.
type fanoutExporter struct {
	exporters []sdktrace.SpanExporter
}

// newFanoutExporter returns a SpanExporter that forwards to all given exporters.
// When only one exporter is provided it is returned directly to avoid overhead.
func newFanoutExporter(exporters []sdktrace.SpanExporter) sdktrace.SpanExporter {
	if len(exporters) == 1 {
		logTracing.Printf("newFanoutExporter: single exporter, bypassing fanout")
		return exporters[0]
	}
	logTracing.Printf("newFanoutExporter: creating fanout exporter with %d backends", len(exporters))
	return &fanoutExporter{exporters: exporters}
}

// forEachExporter calls fn on each underlying exporter concurrently,
// collecting and joining all errors. The op label is used in debug log
// messages to identify which operation is running.
func (f *fanoutExporter) forEachExporter(op string, fn func(sdktrace.SpanExporter) error) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, exp := range f.exporters {
		wg.Add(1)
		go func(e sdktrace.SpanExporter) {
			defer wg.Done()
			if err := fn(e); err != nil {
				logTracing.Printf("fanoutExporter.%s: backend (%T) error: %v", op, e, err)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(exp)
	}
	wg.Wait()
	if len(errs) > 0 {
		logTracing.Printf("fanoutExporter.%s: %d/%d backends failed", op, len(errs), len(f.exporters))
	}
	return errors.Join(errs...)
}

// ExportSpans exports spans to each underlying exporter concurrently. All
// exporters are invoked in parallel so that a slow or hung backend cannot
// delay delivery to the others. Errors from all exporters are collected and
// joined before returning.
func (f *fanoutExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	logTracing.Printf("fanoutExporter.ExportSpans: exporting %d spans to %d backends", len(spans), len(f.exporters))
	return f.forEachExporter("ExportSpans", func(e sdktrace.SpanExporter) error {
		return e.ExportSpans(ctx, spans)
	})
}

// Shutdown shuts down each underlying exporter concurrently, giving each
// exporter its own context derived from the parent's remaining deadline.
// This prevents a slow or unresponsive exporter from consuming the entire
// deadline and leaving insufficient time for other exporters to complete.
func (f *fanoutExporter) Shutdown(ctx context.Context) error {
	logTracing.Printf("fanoutExporter.Shutdown: shutting down %d backends", len(f.exporters))

	// Capture the remaining time budget from the parent context once so that
	// every goroutine starts with the same deadline, even if Go's scheduler
	// delays some goroutines after others begin executing.
	var remaining time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline)
	}

	err := f.forEachExporter("Shutdown", func(e sdktrace.SpanExporter) error {
		// When there is no deadline, use the parent context as-is so that
		// parent cancellation still propagates.
		if remaining <= 0 {
			return e.Shutdown(ctx)
		}
		// Give each exporter a fresh, independent context with the remaining
		// budget so one slow exporter cannot starve the others.
		exporterCtx, cancel := context.WithTimeout(ctx, remaining)
		defer cancel()
		return e.Shutdown(exporterCtx)
	})
	logTracing.Printf("fanoutExporter.Shutdown: completed")
	return err
}
