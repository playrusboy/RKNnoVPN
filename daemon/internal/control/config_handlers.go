package control

import (
	"encoding/json"
	"errors"
	"fmt"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

type CurrentConfigFunc func() *config.Config

type PersistConfigMutationFunc func(*config.Config, bool, string) (applytx.ConfigTransactionResult, error)

type ConfigHandlers struct {
	CurrentConfig         CurrentConfigFunc
	PersistConfigMutation PersistConfigMutationFunc
	RuntimeStatus         RuntimeStatusFunc
}

func (h ConfigHandlers) ConfigList(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	current, rpcErr := h.currentConfig()
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(current)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: err.Error()}
	}

	var full map[string]interface{}
	if err := json.Unmarshal(data, &full); err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: err.Error()}
	}

	keys := make(map[string]string)
	for k, v := range full {
		keys[k] = fmt.Sprintf("%T", v)
	}
	return keys, nil
}

func (h ConfigHandlers) ConfigImport(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	current, rpcErr := h.currentConfig()
	if rpcErr != nil {
		return nil, rpcErr
	}
	newCfg, err := DecodeConfigImportParams(params, current.Profile)
	if err != nil {
		code := ipc.CodeInvalidParams
		var requestErr ConfigImportError
		if errors.As(err, &requestErr) && requestErr.Kind == ConfigImportInvalidConfig {
			code = ipc.CodeConfigError
		}
		return nil, &ipc.RPCError{Code: code, Message: err.Error()}
	}

	mutation, err := h.persistConfigMutation(newCfg, true, "config-import")
	if err != nil {
		return nil, MutationRPCErrorSaved("config-import", err, mutation.ConfigSaved, h.RuntimeStatus)
	}
	return MutationSuccess("config-import", "imported", true, mutation.RuntimeWasRunning, -1, h.RuntimeStatus), nil
}

func (h ConfigHandlers) currentConfig() (*config.Config, *ipc.RPCError) {
	if h.CurrentConfig == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "config state provider is not configured"}
	}
	current := h.CurrentConfig()
	if current == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "config state is not available"}
	}
	return current, nil
}

func (h ConfigHandlers) persistConfigMutation(nextCfg *config.Config, reload bool, action string) (applytx.ConfigTransactionResult, error) {
	if h.PersistConfigMutation == nil {
		return applytx.ConfigTransactionResult{}, fmt.Errorf("config mutation callback is not configured")
	}
	return h.PersistConfigMutation(nextCfg, reload, action)
}
