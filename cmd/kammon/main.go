package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kammon.bytecoder.nl/internal/audit"
	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/kamfake"
	"kammon.bytecoder.nl/internal/poll"
	"kammon.bytecoder.nl/internal/store"
	"kammon.bytecoder.nl/internal/web"
)

func main() {
	configPath := flag.String("config", "kammon.yml", "path to kammon.yml")
	demo := flag.Bool("demo", false, "run against a fake KAM with generated data")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		if err := runVersion(os.Stdout); err != nil {
			slog.Error("kammon stopped", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*configPath, *demo); err != nil {
		slog.Error("kammon stopped", "error", err)
		os.Exit(1)
	}
}

// openAudit opens the persistent trail, or a memory trail in demo mode or when the file
// cannot be opened (MON-AT-12, MON-AT-19).
func openAudit(cfg config.Config, demo bool) *audit.Log {
	if demo {
		return audit.NewMemory(audit.DemoNote)
	}
	l, err := audit.Open(cfg.AuditFile)
	if err != nil {
		slog.Error("audit trail is not persistent", "path", cfg.AuditFile, "error", err)
		return audit.NewMemory(audit.UnopenedNote)
	}
	return l
}

func run(configPath string, demo bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var cfg config.Config
	if demo {
		cfg = config.Demo()
		// The demo keeps its own generated data and login list, but it still honours the
		// guest_access key from kammon.yml so the Guest button follows the operator's choice
		// (MON-AX-24).
		cfg.GuestAccess = config.GuestAccessFrom(configPath)
		kamURL, err := kamfake.Serve(ctx, kamfake.New(time.Now, time.Now().UnixNano()))
		if err != nil {
			return err
		}
		cfg.KamMonitorURL = kamURL
		slog.Info("demo mode: fake KAM with generated data", "kam", kamURL)
	} else {
		var err error
		if cfg, err = config.Load(configPath); err != nil {
			return err
		}
		for _, u := range cfg.Users {
			slog.Info("user", "user", u)
		}
	}

	st := store.New(store.WithFreshness(cfg.Freshness.StaleAfter, cfg.Freshness.DownAfter))
	// One client for both the poller and the pages' admin actions (D28: one address serves both).
	kamClient := kam.NewHTTP(cfg.KamMonitorURL, cfg.Poll.Timeout)
	poller := &poll.Poller{
		Reader:    kamClient,
		Store:     st,
		Every:     cfg.Poll.Every(),
		Timeout:   cfg.Poll.Timeout,
		NotServed: cfg.Poll.NotServed,
	}
	go poller.Run(ctx)

	// The audit trail records admin changes and sign-ins (docs/requirements/AuditTrail.md).
	// A trail that cannot be opened on disk still runs, in memory, and the page says so.
	auditLog := openAudit(cfg, demo)
	defer auditLog.Close()

	srv, err := web.New(web.Deps{Config: cfg, ConfigPath: configPath, Store: st, Demo: demo, KAM: kamClient, Audit: auditLog})
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: cfg.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	slog.Info("KAMmon listening", "address", cfg.Listen, "tls", cfg.TLSCert != "", "kam", cfg.KamMonitorURL)
	if cfg.TLSCert != "" {
		if err := httpServer.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
