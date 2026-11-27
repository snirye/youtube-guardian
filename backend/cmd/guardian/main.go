package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/snirye/youtube-guardian/backend/internal/api"
	"github.com/snirye/youtube-guardian/backend/internal/config"
	"github.com/snirye/youtube-guardian/backend/internal/database"
	"github.com/snirye/youtube-guardian/backend/internal/store"
	"github.com/snirye/youtube-guardian/backend/internal/telegram"
)

func main() {
	configPath := flag.String("config", "guardian.json", "path to the backend JSON configuration")
	telegramMode := flag.String("telegram-setup", "", "pair an existing Telegram bot or create a managed one (pair or managed)")
	flag.Parse()

	var err error
	if *telegramMode != "" {
		err = setupTelegram(*telegramMode)
	} else {
		err = start(*configPath)
	}
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func foreground(configPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, configPath, nil, nil)
}

func run(ctx context.Context, configPath string, telegramTransport http.RoundTripper, ready func()) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	db, err := database.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", cfg.ListenAddress())
	if err != nil {
		return fmt.Errorf("listen on loopback: %w", err)
	}
	defer listener.Close()

	data := store.Store{DB: db}
	workers, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	if cfg.TelegramToken != "" {
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			telegram.New(data, cfg.TelegramToken, cfg.ParentID, telegramTransport).Run(workers)
		}()
		defer func() {
			cancelWorkers()
			<-stopped
		}()
	}
	pruned := make(chan struct{})
	go func() {
		defer close(pruned)
		pruneActivity(workers, data, cfg.RetentionDays)
	}()
	defer func() {
		cancelWorkers()
		<-pruned
	}()

	server := &http.Server{
		Handler:           api.New(db, cfg.APIToken, api.Routes(data)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	result := make(chan error, 1)
	log.Printf("YouTube Guardian listening on %s", listener.Addr())
	go func() {
		result <- server.Serve(listener)
	}()

	if ready != nil {
		ready()
	}

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func pruneActivity(ctx context.Context, data store.Store, days int) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := data.Prune(ctx, time.Now().AddDate(0, 0, -days)); err != nil && ctx.Err() == nil {
			log.Printf("prune activity events: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
