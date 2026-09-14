package problem

import "net/http"

// BaseURI is the canonical prefix for DCM problem type identifiers (RFC 9457).
const BaseURI = "https://dcm-project.github.io/problems/"

const (
	TypeInvalidArgument     = BaseURI + "invalid-argument"
	TypeUnauthenticated     = BaseURI + "unauthenticated"
	TypePermissionDenied    = BaseURI + "permission-denied"
	TypeNotFound            = BaseURI + "not-found"
	TypeAlreadyExists       = BaseURI + "already-exists"
	TypeFailedPrecondition  = BaseURI + "failed-precondition"
	TypeUnprocessableEntity = BaseURI + "unprocessable-entity"
	TypeInternal            = BaseURI + "internal"
	TypeUnavailable         = BaseURI + "unavailable"
	TypeConflict            = BaseURI + "conflict"
)

// ProblemFields holds RFC 9457 problem detail fields for HTTP error responses.
type ProblemFields struct {
	Type   string
	Status int
	Title  string
	Detail string
}

// TitleForStatus returns the standard RFC 9457 title for common HTTP status codes.
func TitleForStatus(status int) string {
	return http.StatusText(status)
}

// AuthErrorFields maps auth middleware status codes to problem type URIs.
func AuthErrorFields(status int, detail string) ProblemFields {
	var errType string
	switch {
	case status == http.StatusForbidden:
		errType = TypePermissionDenied
	case status == http.StatusConflict:
		errType = TypeAlreadyExists
	case status >= 500:
		errType = TypeInternal
	default:
		errType = TypeUnauthenticated
	}
	return ProblemFields{
		Type:   errType,
		Status: status,
		Title:  TitleForStatus(status),
		Detail: detail,
	}
}

// ValidationErrorFields returns fields for OpenAPI request validation failures.
func ValidationErrorFields(status int, detail string) ProblemFields {
	errType := TypeInvalidArgument
	if status == http.StatusUnauthorized {
		errType = TypeUnauthenticated
	}
	return ProblemFields{
		Type:   errType,
		Status: status,
		Title:  TitleForStatus(status),
		Detail: detail,
	}
}
