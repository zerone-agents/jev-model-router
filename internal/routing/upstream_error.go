package routing

// ErrGenerationCapacity keeps the existing public Chat error classification
// while allowing another protocol to distinguish Router capacity from a
// failed external provider.
var ErrGenerationCapacity = Fail("upstream_error", "generation connection capacity reached")

// UpstreamError carries only the public provider error, never SDK diagnostics.
// Unwrap keeps internal records and management failures on the stable router code.
type UpstreamError struct {
	// ReportedCode is populated only from a verified upstream envelope/event.
	// Body may instead contain locally synthesized protocol fields.
	ReportedCode string
	Status       int
	Body         map[string]any
}

func (e *UpstreamError) Error() string { return "upstream_error: generation provider failed" }
func (e *UpstreamError) Unwrap() error { return Fail("upstream_error", "generation provider failed") }
