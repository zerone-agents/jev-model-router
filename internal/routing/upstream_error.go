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
	RetryAfter   string // Verified upstream header; internal only.
	Details      *UpstreamDetails
	ReportedCode string
	Status       int
	Body         map[string]any
}

func (e *UpstreamError) Error() string { return "upstream_error: generation provider failed" }
func (e *UpstreamError) Unwrap() error { return Fail("upstream_error", "generation provider failed") }

// TransportFailure classifies a verified transport/protocol failure without
// retaining the SDK error, which may contain credentials or raw request data.
type TransportFailure struct{ Cause error }

func (e *TransportFailure) Error() string { return e.Cause.Error() }
func (e *TransportFailure) Unwrap() error { return e.Cause }
func TemporaryUpstream(e error) error     { return &TransportFailure{Cause: e} }
