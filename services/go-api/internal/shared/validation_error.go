package shared

// ValidationError is a 400 Bad Request raised when a domain invariant is
// violated. Its ErrorCode is namespaced by a per-context prefix so the wire
// output stays stable across packages. It returns an int status and never
// imports net/http, so domain packages may depend on it without breaking the
// domain-purity import rules.
type ValidationError struct {
	codePrefix string
	message    string
}

// NewValidationError builds a validation error whose ErrorCode is
// "<codePrefix>.validation_error".
func NewValidationError(codePrefix, message string) *ValidationError {
	return &ValidationError{codePrefix: codePrefix, message: message}
}

func (e *ValidationError) Error() string     { return e.message }
func (e *ValidationError) HTTPStatus() int   { return 400 }
func (e *ValidationError) ErrorCode() string { return e.codePrefix + ".validation_error" }
