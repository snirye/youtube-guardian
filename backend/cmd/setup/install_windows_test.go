//go:build guardian_setup

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/snirye/youtube-guardian/backend/internal/database"
	"github.com/snirye/youtube-guardian/backend/internal/store"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/mgr"
)

func TestInstallAndRepairOnWindows(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("this test changes system service and Chrome policy on a clean Windows CI runner")
	}
	p, err := systemPaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{p.program, p.data} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected a clean Windows runner at %s: %v", path, err)
		}
	}
	scm, err := mgr.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer scm.Disconnect()
	if service, err := scm.OpenService(serviceName); !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		if service != nil {
			service.Close()
		}
		t.Fatalf("expected no existing Guardian service: %v", err)
	}
	keyPath := chromePolicyPath()
	if key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.QUERY_VALUE|registry.WOW64_64KEY); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		if err == nil {
			key.Close()
		}
		t.Fatalf("expected no existing Chrome policy: %v", err)
	}
	defer func() {
		if service, err := scm.OpenService(serviceName); err == nil {
			stopService(service)
			service.Delete()
			service.Close()
		}
		registry.DeleteKey(registry.LOCAL_MACHINE, keyPath)
		os.RemoveAll(p.program)
		os.RemoveAll(p.data)
	}()

	// Exercise the fresh configuration layout without contacting Telegram from CI.
	cfg, err := loadOrCreateConfig(p, "123:fixture_token_is_not_real", 456)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TelegramToken, cfg.ParentID = "", 0
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.config, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := install(p, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertApproval := func() {
		t.Helper()
		var snapshot store.Snapshot
		if err := localRequest(installed, "/config", &snapshot); err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(snapshot.Channels, func(channel store.Channel) bool {
			return channel.ID == "UC1234567890123456789012" && channel.Decision == "approved"
		}) {
			t.Fatal("approved channel missing after install")
		}
	}

	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(p.data, "guardian.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	err = (store.Store{DB: db}).Decide(ctx, "UC1234567890123456789012", "Fixture channel", true)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	assertApproval()

	repaired, err := install(p, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.APIToken != installed.APIToken || repaired.Port != installed.Port {
		t.Fatal("repair changed the existing local connection settings")
	}
	assertApproval()

	var beat struct{ ConnectedAt *string }
	if err := localRequest(repaired, "/heartbeat", &beat); err != nil || beat.ConnectedAt != nil {
		t.Fatalf("expected no Chrome check-in yet: %+v, %v", beat, err)
	}
	res, err := http.Get("http://" + repaired.ListenAddress() + "/heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("heartbeat status without token = %d", res.StatusCode)
	}
}
