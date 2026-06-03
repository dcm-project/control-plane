package v1alpha1

import (
	"errors"

	v1alpha1 "github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/internal/policy/api/server"
	"github.com/dcm-project/control-plane/internal/policy/store"
)

func isInvalidPageToken(err error) bool {
	return errors.Is(err, store.ErrInvalidPageToken)
}

func invalidPageTokenBadRequest(err error) server.BadRequestJSONResponse {
	return server.BadRequestJSONResponse(v1alpha1.Error{
		Type:   v1alpha1.INVALIDARGUMENT,
		Status: 400,
		Title:  "Bad Request",
		Detail: strPtr(err.Error()),
	})
}
