package v1alpha1

import (
	"context"
	"fmt"

	"github.com/dcm-project/control-plane/internal/catalog/api/server"
)

func (h *Handler) GetHealth(_ context.Context, _ server.GetHealthRequestObject) (server.GetHealthResponseObject, error) {
	status := "ok"
	path := fmt.Sprintf("%shealth", apiPrefix)
	return server.GetHealth200JSONResponse{
		Status: status,
		Path:   &path,
	}, nil
}
