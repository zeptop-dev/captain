// Command captain is the unified panel.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.com/boyang-hu/captain/internal/config"
	"gitlab.com/boyang-hu/captain/internal/db"
	chttp "gitlab.com/boyang-hu/captain/internal/http"
	"gitlab.com/boyang-hu/captain/internal/http/admin"
	"gitlab.com/boyang-hu/captain/internal/store"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "migrate":
		err = cmdMigrate(os.Args[2:])
	case "admin":
		err = cmdAdmin(os.Args[2:])
	case "version":
		fmt.Println("captain", version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "captain:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  captain serve   -c config.yaml                      run the panel
  captain migrate -c config.yaml                      apply database migrations
  captain admin create -c config.yaml -email E -password P   create an admin user
  captain version`)
}

func open(cfgPath string) (*config.Config, *store.Store, *slog.Logger, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, nil, err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return nil, nil, nil, fmt.Errorf("log_level: %w", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	conn, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := db.Migrate(context.Background(), conn, cfg.Database.Driver); err != nil {
		return nil, nil, nil, fmt.Errorf("migrate: %w", err)
	}
	return cfg, store.New(conn), log, nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfgPath := fs.String("c", "/etc/captain/config.yaml", "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, st, log, err := open(*cfgPath)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: chttp.New(cfg, st, log).Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("captain listening", "addr", cfg.Listen, "base_url", cfg.BaseURL, "version", version)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	cfgPath := fs.String("c", "/etc/captain/config.yaml", "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, log, err := open(*cfgPath)
	if err != nil {
		return err
	}
	log.Info("migrations applied")
	return nil
}

func cmdAdmin(args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("admin: only 'create' is supported")
	}
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	cfgPath := fs.String("c", "/etc/captain/config.yaml", "config file")
	email := fs.String("email", "", "admin email")
	password := fs.String("password", "", "admin password")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" || len(*password) < 8 {
		return fmt.Errorf("admin create: -email and a -password of 8+ chars are required")
	}
	_, st, log, err := open(*cfgPath)
	if err != nil {
		return err
	}
	u, err := admin.NewUser(*email, *password, "admin")
	if err != nil {
		return err
	}
	if err := st.CreateUser(context.Background(), u); err != nil {
		return err
	}
	log.Info("admin created", "id", u.ID, "email", u.Email)
	return nil
}
