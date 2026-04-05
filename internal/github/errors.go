package github

import "fmt"

// PermanentError wraps an error that should not be retried.
// Auth failures, invalid payloads, missing resources.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// IsPermanent returns true if the error should not be retried.
func IsPermanent(err error) bool {
	for err != nil {
		if _, ok := err.(*PermanentError); ok {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func permanent(format string, args ...any) error {
	return &PermanentError{Err: fmt.Errorf(format, args...)}
}
