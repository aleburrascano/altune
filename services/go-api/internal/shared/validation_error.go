package shared

type ValidationError struct {
	codePrefix string
	message    string
}

func NewValidationError(codePrefix, message string) *ValidationError {
	return &ValidationError{codePrefix: codePrefix, message: message}
}

func (e *ValidationError) Error() string     { return e.message }
func (e *ValidationError) HTTPStatus() int   { return 400 }
func (e *ValidationError) ErrorCode() string { return e.codePrefix + ".validation_error" }
