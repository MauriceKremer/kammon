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
	flag.Parse()

	if err := run(*configPath, *demo); err != nil {
		slog.Error("kammon stopped", "error", err)
		os.Exit(1)
	}
}

func run(configPath string, demo bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var cfg config.Config
	if demo {
		cfg = config.Demo()
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

	st := store.New()
	poller := &poll.Poller{Reader: kam.NewHTTP(cfg.KamMonitorURL, 3*time.Second), Store: st}
	go poller.Run(ctx)

	srv, err := web.New(web.Deps{Config: cfg, Store: st, Demo: demo})
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
	slog.Info("KAMmon listening", "address", cfg.Listen, "kam", cfg.KamMonitorURL)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
