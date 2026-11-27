//go:build guardian_setup

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snirye/youtube-guardian/backend/internal/config"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

//go:embed payload.zip
var payload []byte

const extensionID = "jcpbnaipmnmgfoelllffgphnfbbngdik"

var localHTTP = &http.Client{
	Timeout:   3 * time.Second,
	Transport: &http.Transport{},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func localRequest(cfg config.Config, path string, result any) error {
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.ListenAddress()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	res, err := localHTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("local service returned HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(result)
}

func stagePayload(programParent string) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(programParent, "YouTubeGuardian-setup-")
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(stage)
	if err != nil {
		os.RemoveAll(stage)
		return "", err
	}
	defer root.Close()
	err = unpackPayload(root, archive)
	if err != nil {
		root.Close()
		os.RemoveAll(stage)
		return "", err
	}
	return stage, nil
}

func unpackPayload(root *os.Root, archive *zip.Reader) error {
	if err := root.Mkdir("extension", 0755); err != nil {
		return err
	}
	for _, entry := range archive.File {
		parent := filepath.Dir(filepath.FromSlash(entry.Name))
		if !entry.Mode().IsRegular() ||
			(entry.Name != "guardian.exe" && (parent != "extension" || strings.Contains(entry.Name, `\`))) {
			return fmt.Errorf("unexpected installer payload entry: %s", entry.Name)
		}
		src, err := entry.Open()
		if err != nil {
			return err
		}
		dst, err := root.OpenFile(entry.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		if err := errors.Join(copyErr, dst.Close()); err != nil {
			return err
		}
	}
	for _, name := range []string{"guardian.exe", "extension/manifest.json"} {
		if _, err := root.Stat(name); err != nil {
			return err
		}
	}
	return nil
}

func loadOrCreateConfig(p installPaths, token string, parent int64) (config.Config, error) {
	exists, err := p.hasConfiguration()
	if err != nil {
		return config.Config{}, err
	}
	if exists {
		cfg, err := config.Load(p.config)
		if err != nil {
			return config.Config{}, err
		}
		if !strings.EqualFold(cfg.DatabasePath, filepath.Join(p.data, "guardian.sqlite")) {
			return config.Config{}, errors.New("the existing database path is outside the default installation layout")
		}
		return cfg, nil
	}
	if token == "" || parent <= 0 {
		return config.Config{}, errors.New("pair a Telegram account before installing")
	}
	if err := createDataDirectory(p.data); err != nil {
		return config.Config{}, err
	}
	var secret [32]byte
	rand.Read(secret[:])
	cfg := config.Config{
		Port: config.DefaultPort, DatabasePath: "guardian.sqlite", APIToken: hex.EncodeToString(secret[:]),
		TelegramToken: token, ParentID: parent, RetentionDays: 30,
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err == nil {
		err = os.WriteFile(p.config, encoded, 0600)
	}
	if err != nil {
		os.Remove(p.config)
		os.Remove(p.data)
	}
	return cfg, err
}

func install(p installPaths, token string, parent int64) (cfg config.Config, result error) {
	scm, err := mgr.Connect()
	if err != nil {
		return cfg, err
	}
	defer scm.Disconnect()
	service, err := scm.OpenService(serviceName)
	existing := err == nil
	if err != nil && !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return cfg, err
	}
	binary := filepath.Join(p.program, "guardian.exe")
	if existing {
		defer service.Close()
		current, err := service.Config()
		if err != nil {
			return cfg, err
		}
		args, err := windows.DecomposeCommandLine(current.BinaryPathName)
		if err != nil || len(args) != 3 || !strings.EqualFold(args[0], binary) ||
			args[1] != "-config" || !strings.EqualFold(args[2], p.config) {
			return cfg, errors.New("the existing Windows service uses a different installation path")
		}
	}
	cfg, err = loadOrCreateConfig(p, token, parent)
	if err != nil {
		return cfg, err
	}
	stage, err := stagePayload(filepath.Dir(p.program))
	if err != nil {
		return cfg, err
	}
	defer os.RemoveAll(stage)
	if err := plainPath(p.program); err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	backupDir, err := os.MkdirTemp(filepath.Dir(p.program), "YouTubeGuardian-backup-")
	if err != nil {
		return cfg, err
	}
	backup := filepath.Join(backupDir, "previous")
	movedOld := false
	movedNew := false
	wasRunning := false
	created := false
	defer func() {
		if created {
			defer service.Close()
		}
		if result == nil {
			os.RemoveAll(backupDir)
			return
		}
		var rollback error
		if movedNew && service != nil {
			rollback = stopService(service)
		}
		if created && rollback == nil {
			rollback = service.Delete()
		}
		if movedNew && rollback == nil {
			rollback = os.RemoveAll(p.program)
		}
		if movedOld && rollback == nil {
			rollback = os.Rename(backup, p.program)
		}
		if existing && wasRunning && rollback == nil {
			rollback = startService(service, cfg)
		}
		if rollback != nil {
			result = errors.Join(result, fmt.Errorf("restore previous installation (backup directory %s): %w", backupDir, rollback))
		} else {
			os.Remove(backupDir)
		}
	}()
	if existing {
		status, err := service.Query()
		if err != nil {
			return cfg, err
		}
		wasRunning = status.State != svc.Stopped
		if err := stopService(service); err != nil {
			// The service still owns the original files.
			wasRunning = false
			return cfg, err
		}
	}
	if err := os.Rename(p.program, backup); err == nil {
		movedOld = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if err := os.Rename(stage, p.program); err != nil {
		return cfg, err
	}
	movedNew = true
	if !existing {
		service, err = scm.CreateService(serviceName, binary, mgr.Config{
			DisplayName: "YouTube Guardian", Description: "Local backend for YouTube Guardian",
			StartType: mgr.StartAutomatic, ServiceStartName: `NT AUTHORITY\LocalService`,
		}, "-config", p.config)
		if err != nil {
			return cfg, err
		}
		created = true
		if err := service.SetRecoveryActions([]mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		}, 86400); err != nil {
			return cfg, err
		}
		if err := service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
			return cfg, err
		}
	}
	if err := ensureEventSource(); err != nil {
		return cfg, err
	}
	if err := startService(service, cfg); err != nil {
		return cfg, err
	}
	if err := writeChromePolicy(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func ensureEventSource() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\EventLog\Application\`+serviceName, registry.QUERY_VALUE)
	if err == nil {
		return key.Close()
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return err
	}
	return eventlog.InstallAsEventCreate(serviceName, eventlog.Info|eventlog.Warning|eventlog.Error)
}

func chromePolicyPath() string {
	return `SOFTWARE\Policies\Google\Chrome\3rdparty\extensions\` + extensionID + `\policy`
}

func writeChromePolicy(cfg config.Config) error {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, chromePolicyPath(), registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetStringValue("apiToken", cfg.APIToken); err != nil {
		return err
	}
	return key.SetDWordValue("port", uint32(cfg.Port))
}

func stopService(service *mgr.Service) error {
	status, err := service.Query()
	if err != nil || status.State == svc.Stopped {
		return err
	}
	if status.State != svc.StopPending {
		if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return retry(ctx, func() error {
		status, err := service.Query()
		if err == nil && status.State != svc.Stopped {
			return errors.New("the service is still stopping")
		}
		return err
	})
}

func startService(service *mgr.Service, cfg config.Config) error {
	if err := service.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return retry(ctx, func() error {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State != svc.Running {
			return errors.New("the Windows service is not running")
		}
		var health struct{ Status string }
		if err := localRequest(cfg, "/health", &health); err != nil {
			return err
		}
		if health.Status != "ok" {
			return errors.New("the local service health check failed")
		}
		return nil
	})
}

func retry(ctx context.Context, check func() error) error {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := check()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
		}
	}
}
