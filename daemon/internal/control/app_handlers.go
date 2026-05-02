package control

import (
	"encoding/json"
	"fmt"

	appcatalog "github.com/youtubediscord/RKNnoVPN/daemon/internal/apps"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

type AppHandlers struct {
	PackagesListPath string
	Catalog          *appcatalog.Catalog
}

var defaultAppCatalog = &appcatalog.Catalog{}

func (h AppHandlers) AppList(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	apps, err := h.loadInstalled()
	if err != nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInternalError,
			Message: "load apps failed: " + err.Error(),
		}
	}
	return apps, nil
}

func (h AppHandlers) ResolveUID(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeResolveUIDParams(params)
	if err != nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInvalidParams,
			Message: err.Error(),
		}
	}

	apps, err := h.loadInstalled()
	if err != nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInternalError,
			Message: "load apps failed: " + err.Error(),
		}
	}

	if app, ok := appcatalog.ResolveUID(apps, request.UID); ok {
		return app, nil
	}

	return nil, &ipc.RPCError{
		Code:    ipc.CodeInvalidParams,
		Message: fmt.Sprintf("no package found for uid %d", request.UID),
	}
}

func (h AppHandlers) loadInstalled() ([]appcatalog.Info, error) {
	catalog := h.Catalog
	if catalog == nil {
		catalog = defaultAppCatalog
	}
	return catalog.LoadInstalled(h.packagesListPath())
}

func (h AppHandlers) packagesListPath() string {
	if h.PackagesListPath != "" {
		return h.PackagesListPath
	}
	return appcatalog.DefaultPackagesListPath
}
