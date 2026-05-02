package control

import (
	"context"
	"testing"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
)

func TestConfigImportContextCancelledSkipsPersistMutation(t *testing.T) {
	persistCalled := false
	handler := ConfigHandlers{
		CurrentConfig: func() *config.Config {
			t.Fatal("cancelled config-import should not read current config")
			return nil
		},
		PersistConfigMutation: func(*config.Config, bool, string) (applytx.ConfigTransactionResult, error) {
			persistCalled = true
			return applytx.ConfigTransactionResult{}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, rpcErr := handler.ConfigImportContext(ctx, nil); rpcErr == nil {
		t.Fatal("expected cancelled context error")
	}
	if persistCalled {
		t.Fatal("cancelled config-import should not persist config")
	}
}
