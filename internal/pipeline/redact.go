package pipeline

// redactedError carries a job error's redacted text as its message and
// unwraps to the job error, so errors.Is and errors.As still see the cause.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Unwrap() error { return e.err }
