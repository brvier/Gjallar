// Gjallar is a KISS monitoring service: one binary, one YAML config file,
// one SQLite file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gjallar/internal/alert"
	"gjallar/internal/check"
	"gjallar/internal/config"
	"gjallar/internal/store"
	"gjallar/internal/web"
)

func main() {
	configPath := flag.String("config", "gjallar.yaml", "path to YAML configuration")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if err := run(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, "gjallar:", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Build every checker up front so a bad rule or URL fails at startup.
	checkers := make([]check.Checker, len(cfg.Monitors))
	for i, m := range cfg.Monitors {
		if checkers[i], err = check.New(m); err != nil {
			return err
		}
	}
	if err := pingSelfTest(cfg); err != nil {
		return err
	}

	notifiers, err := alert.BuildNotifiers(cfg.Alerts)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer st.Close()

	engine, err := alert.NewEngine(cfg, st, notifiers)
	if err != nil {
		return err
	}

	srv, err := web.New(cfg, st)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Pipeline: one goroutine per monitor -> results channel -> single
	// consumer (one SQLite writer, lock-free alert state machine).
	results := make(chan check.Result, len(cfg.Monitors))
	var runners sync.WaitGroup
	for i, m := range cfg.Monitors {
		runners.Add(1)
		go func(m config.Monitor, c check.Checker) {
			defer runners.Done()
			check.Run(ctx, m, c, results)
		}(m, checkers[i])
	}
	go func() {
		runners.Wait()
		close(results)
	}()

	var consumer sync.WaitGroup
	consumer.Add(1)
	go func() {
		defer consumer.Done()
		for r := range results {
			slog.Info("check", "monitor", r.Monitor, "ok", r.OK, "latency", r.Latency, "message", r.Message)
			if err := st.InsertResult(r.Monitor, r.Time, r.OK, r.Latency, r.Message); err != nil {
				slog.Error("storing result", "monitor", r.Monitor, "error", err)
			}
			engine.Process(r)
		}
	}()

	go pruneLoop(ctx, st, cfg.Retention.D())

	httpSrv := &http.Server{Addr: cfg.Listen, Handler: srv.Handler()}
	go func() {
		slog.Info("status page listening", "addr", cfg.Listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	// Order: stop check loops, drain the pipeline, stop HTTP, then close the DB.
	runners.Wait()
	consumer.Wait()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "error", err)
	}
	return nil
}

func pingSelfTest(cfg *config.Config) error {
	tested := map[bool]bool{}
	for _, m := range cfg.Monitors {
		if m.Type == "ping" && !tested[m.Privileged] {
			tested[m.Privileged] = true
			if err := check.SelfTestPing(m.Privileged); err != nil {
				return err
			}
		}
	}
	return nil
}

func pruneLoop(ctx context.Context, st *store.Store, retention time.Duration) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := st.Prune(time.Now().Add(-retention)); err != nil {
			slog.Error("pruning", "error", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
