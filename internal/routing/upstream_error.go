package routing

// ErrGenerationCapacity keeps the existing public Chat error classification
// while allowing another protocol to distinguish Router capacity from a
// failed external provider.
var ErrGenerationCapacity = Fail("upstream_error", "generation connection capacity reached")

// UpstreamError carries only the public provider error, never SDK diagnostics.
// Unwrap keeps internal records and management failures on the stable router code.
type UpstreamError struct {
	Status int
	Body   map[string]any
}

func (e *UpstreamError) Error() string { return "upstream_error: generation provider failed" }
func (e *UpstreamError) Unwrap() error { return Fail("upstream_error", "generation provider failed") }
