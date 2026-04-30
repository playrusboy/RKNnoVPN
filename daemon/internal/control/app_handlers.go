package control

import (
	"encoding/json"
	"fmt"

	appcatalog "github.com/youtubediscord/RKNnoVPN/daemon/internal/apps"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

type AppHandlers struct {
	PackagesListPath string
}

func (h AppHandlers) AppList(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	apps, err := appcatalog.LoadInstalled(h.packagesListPath())
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

	apps, err := appcatalog.LoadInstalled(h.packagesListPath())
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

func (h AppHandlers) packagesListPath() string {
	if h.PackagesListPath != "" {
		return h.PackagesListPath
	}
	return appcatalog.DefaultPackagesListPath
}
