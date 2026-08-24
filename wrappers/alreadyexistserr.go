package wrappers

// AlreadyExistsErr is an error of type alreadyExistsError with the underlying error message
var AlreadyExistsErr error = alreadyExistsError{msg: "resource already exists"}

// alreadyExistsError is an implementation of error interface
type alreadyExistsError struct {
	msg string
}

// NewAlreadyExistsErr wraps the given error in an alreadyExistsError
func NewAlreadyExistsErr(err error) error {
	if err == nil {
		return nil
	}

	return alreadyExistsError{
		msg: err.Error(),
	}
}

// Error returns the error message
func (e alreadyExistsError) Error() string {
	return e.msg
}

// Is returns true if the target error is an alreadyExistsError
func (e alreadyExistsError) Is(tgt error) bool {
	_, ok := tgt.(alreadyExistsError)
	return ok
}
