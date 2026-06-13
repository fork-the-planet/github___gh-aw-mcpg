package tracing

import (
	"context"
	"errors"
	"sync"

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
		return exporters[0]
	}
	return &fanoutExporter{exporters: exporters}
}

// forEachExporter calls fn on each underlying exporter concurrently,
// collecting and joining all errors.
func (f *fanoutExporter) forEachExporter(fn func(sdktrace.SpanExporter) error) error {
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
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(exp)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// ExportSpans exports spans to each underlying exporter concurrently. All
// exporters are invoked in parallel so that a slow or hung backend cannot
// delay delivery to the others. Errors from all exporters are collected and
// joined before returning.
func (f *fanoutExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return f.forEachExporter(func(e sdktrace.SpanExporter) error {
		return e.ExportSpans(ctx, spans)
	})
}

// Shutdown shuts down each underlying exporter concurrently, collecting any
// errors. All errors are joined and returned.
func (f *fanoutExporter) Shutdown(ctx context.Context) error {
	return f.forEachExporter(func(e sdktrace.SpanExporter) error {
		return e.Shutdown(ctx)
	})
}
