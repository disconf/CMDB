package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

func TestRunAssetCacheRefreshHonoursContextCancel(t *testing.T) {
	previous, had := os.LookupEnv("CMDB_ASSET_RELOAD_INTERVAL_SEC")
	_ = os.Setenv("CMDB_ASSET_RELOAD_INTERVAL_SEC", "5")
	defer func() {
		if had {
			_ = os.Setenv("CMDB_ASSET_RELOAD_INTERVAL_SEC", previous)
			return
		}
		_ = os.Unsetenv("CMDB_ASSET_RELOAD_INTERVAL_SEC")
	}()

	// A server without a CMDB service must exit immediately and never panic.
	(&Server{}).RunAssetCacheRefresh(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&Server{cmdb: cmdb.NewService()}).RunAssetCacheRefresh(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunAssetCacheRefresh must stop on context cancel")
	}
}
