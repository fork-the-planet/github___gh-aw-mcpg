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
		logTracing.Printf("newFanoutExporter: single exporter, bypassing fanout")
		return exporters[0]
	}
	logTracing.Printf("newFanoutExporter: creating fanout exporter with %d backends", len(exporters))
	return &fanoutExporter{exporters: exporters}
}

// ExportSpans exports spans to each underlying exporter concurrently. All
// exporters are invoked in parallel so that a slow or hung backend cannot
// delay delivery to the others. Errors from all exporters are collected and
// joined before returning.
func (f *fanoutExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	logTracing.Printf("fanoutExporter.ExportSpans: exporting %d spans to %d backends", len(spans), len(f.exporters))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, exp := range f.exporters {
		wg.Add(1)
		go func(e sdktrace.SpanExporter) {
			defer wg.Done()
			if err := e.ExportSpans(ctx, spans); err != nil {
				logTracing.Printf("fanoutExporter.ExportSpans: backend export error: %v", err)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(exp)
	}
	wg.Wait()
	if len(errs) > 0 {
		logTracing.Printf("fanoutExporter.ExportSpans: %d/%d backends failed", len(errs), len(f.exporters))
	}
	return errors.Join(errs...)
}

// Shutdown shuts down each underlying exporter concurrently, collecting any
// errors. All errors are joined and returned.
func (f *fanoutExporter) Shutdown(ctx context.Context) error {
	logTracing.Printf("fanoutExporter.Shutdown: shutting down %d backends", len(f.exporters))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, exp := range f.exporters {
		wg.Add(1)
		go func(e sdktrace.SpanExporter) {
			defer wg.Done()
			if err := e.Shutdown(ctx); err != nil {
				logTracing.Printf("fanoutExporter.Shutdown: backend shutdown error: %v", err)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(exp)
	}
	wg.Wait()
	logTracing.Printf("fanoutExporter.Shutdown: completed, errors=%d", len(errs))
	return errors.Join(errs...)
}
