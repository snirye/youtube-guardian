package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

const serviceName = "YouTubeGuardian"

func start(path string) error {
	asService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("detect Windows Service mode: %w", err)
	}
	if !asService {
		return foreground(path)
	}
	return svc.Run(serviceName, windowsService{path})
}

type windowsService struct{ configPath string }

func (s windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const controls = svc.AcceptStop | svc.AcceptShutdown
	status := svc.Status{State: svc.StartPending, WaitHint: 15000, CheckPoint: 1}
	changes <- status

	if events, err := eventlog.Open(serviceName); err == nil {
		defer events.Close()
		log.SetOutput(serviceLog{events})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- run(ctx, s.configPath, nil, func() { ready <- struct{}{} })
	}()
	updates := time.NewTicker(2 * time.Second)
	defer updates.Stop()

	for {
		select {
		case <-ready:
			if status.State == svc.StartPending {
				status = svc.Status{State: svc.Running, Accepts: controls}
				changes <- status
			}
		case err := <-finished:
			if err != nil {
				log.Printf("YouTube Guardian stopped: %v", err)
				return true, 1
			}
			return false, 0
		case request, open := <-requests:
			if !open {
				cancel()
				requests = nil
				continue
			}
			switch request.Cmd {
			case svc.Interrogate:
				changes <- status
			case svc.Stop, svc.Shutdown:
				status = svc.Status{State: svc.StopPending, WaitHint: 15000, CheckPoint: 1}
				changes <- status
				cancel()
			}
		case <-updates.C:
			if status.State == svc.StartPending || status.State == svc.StopPending {
				status.CheckPoint++
				changes <- status
			}
		}
	}
}

type serviceLog struct{ events *eventlog.Log }

func (s serviceLog) Write(p []byte) (int, error) {
	if err := s.events.Info(1, string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}
