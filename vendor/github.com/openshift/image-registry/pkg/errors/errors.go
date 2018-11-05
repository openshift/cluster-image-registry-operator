package errors

import (
	"fmt"
	"net/http"

	errcode "github.com/docker/distribution/registry/api/errcode"
)

const errGroup = "openshift"

var (
	ErrorCodePullthroughManifest = errcode.Register(errGroup, errcode.ErrorDescriptor{
		Value:   "OPENSHIFT_PULLTHROUGH_MANIFEST",
		Message: "unable to pull manifest from %s: %v",
		// We have to use an error code within the range [400, 499].
		// Otherwise the error message with not be shown by the client.
		HTTPStatusCode: http.StatusNotFound,
	})
)

// Error provides a wrapper around error.
type Error struct {
	Code    string
	Message string
	Err     error
}

var _ error = Error{}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Message, e.Err.Error())
}

func NewError(code, msg string, err error) *Error {
	return &Error{
		Code:    code,
		Message: msg,
		Err:     err,
	}
}
