// Gjallar is a KISS monitoring service: one binary, one YAML config file,
// one SQLite file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
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

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	configPath := flag.String("config", "gjallar.yaml", "path to YAML configuration")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("gjallar", version)
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	slog.Info("starting gjallar", "version", version)

	if err := run(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, "gjallar:", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	p, err := prepare(cfg)
	if err != nil {
		return err
	}
	inst, err := start(cfg, p)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			inst.stop()
			return nil
		case <-hup:
			// Validate everything we can before touching the running
			// instance: a broken config must not take the service down.
			newCfg, err := config.Load(configPath)
			if err != nil {
				slog.Error("reload failed, keeping current config", "error", err)
				continue
			}
			newP, err := prepare(newCfg)
			if err != nil {
				slog.Error("reload failed, keeping current config", "error", err)
				continue
			}
			inst.stop() // releases the listen port and the SQLite file
			if inst, err = start(newCfg, newP); err != nil {
				return fmt.Errorf("restarting after reload: %w", err)
			}
			slog.Info("configuration reloaded")
		}
	}
}

// prepared holds the parts of an instance that can be built (and fail)
// without owning the listen port or the database.
type prepared struct {
	checkers  []check.Checker
	notifiers map[string]alert.Notifier
}

func prepare(cfg *config.Config) (*prepared, error) {
	p := &prepared{checkers: make([]check.Checker, len(cfg.Monitors))}
	var err error
	for i, m := range cfg.Monitors {
		if p.checkers[i], err = check.New(m); err != nil {
			return nil, err
		}
	}
	if err := pingSelfTest(cfg); err != nil {
		return nil, err
	}
	if p.notifiers, err = alert.BuildNotifiers(cfg.Alerts); err != nil {
		return nil, err
	}
	return p, nil
}

// instance is one running incarnation of the service; SIGHUP stops it and
// starts a fresh one.
type instance struct {
	cancel   context.CancelFunc
	runners  sync.WaitGroup
	consumer sync.WaitGroup
	httpSrv  *http.Server
	st       *store.Store
}

func start(cfg *config.Config, p *prepared) (*instance, error) {
	st, err := store.Open(cfg.Database)
	if err != nil {
		return nil, err
	}
	engine, err := alert.NewEngine(cfg, st, p.notifiers)
	if err != nil {
		st.Close()
		return nil, err
	}
	srv, err := web.New(cfg, st)
	if err != nil {
		st.Close()
		return nil, err
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		st.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	inst := &instance{cancel: cancel, st: st}

	// Pipeline: one goroutine per monitor -> results channel -> single
	// consumer (one SQLite writer, lock-free alert state machine).
	results := make(chan check.Result, len(cfg.Monitors))
	for i, m := range cfg.Monitors {
		inst.runners.Add(1)
		go func(m config.Monitor, c check.Checker) {
			defer inst.runners.Done()
			check.Run(ctx, m, c, results)
		}(m, p.checkers[i])
	}
	go func() {
		inst.runners.Wait()
		close(results)
	}()

	inst.consumer.Add(1)
	go func() {
		defer inst.consumer.Done()
		for r := range results {
			slog.Info("check", "monitor", r.Monitor, "ok", r.OK, "latency", r.Latency, "message", r.Message)
			if err := st.InsertResult(r.Monitor, r.Time, r.OK, r.Latency, r.Message); err != nil {
				slog.Error("storing result", "monitor", r.Monitor, "error", err)
			}
			engine.Process(r)
		}
	}()

	go pruneLoop(ctx, st, cfg.Retention.D())

	inst.httpSrv = &http.Server{Handler: srv.Handler()}
	go func() {
		slog.Info("status page listening", "addr", cfg.Listen)
		if err := inst.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "error", err)
		}
	}()
	return inst, nil
}

// stop tears the instance down in order: stop check loops, drain the
// pipeline, stop HTTP, then close the DB.
func (i *instance) stop() {
	i.cancel()
	i.runners.Wait()
	i.consumer.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := i.httpSrv.Shutdown(ctx); err != nil {
		slog.Error("http shutdown", "error", err)
	}
	if err := i.st.Close(); err != nil {
		slog.Error("closing store", "error", err)
	}
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
