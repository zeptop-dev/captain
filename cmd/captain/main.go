// Command captain is the unified panel.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/zeptop-dev/captain/internal/certs"
	"github.com/zeptop-dev/captain/internal/mail"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	chttp "github.com/zeptop-dev/captain/internal/http"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/jobs"
	"github.com/zeptop-dev/captain/internal/store"
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
	cfg.Version = version
	web := chttp.New(cfg, st, log)
	srv := &http.Server{Addr: cfg.Listen, Handler: web.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go (&jobs.Runner{Store: st, Log: log, BackupDir: filepath.Join(cfg.DataDir, "backups"),
		Mail: &mail.Loader{Store: st}, SiteName: cfg.SiteName, PortalURL: strings.TrimRight(cfg.BaseURL, "/") + "/portal/"}).Run(ctx)

	var httpSrv *http.Server // port 80 helper when serving HTTPS ourselves
	if cfg.TLSEnabled() {
		domain := cfg.TLSDomain()
		if domain == "" {
			return fmt.Errorf("tls: cannot derive a domain from base_url %q; set tls.domain", cfg.BaseURL)
		}
		redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://"+domain+r.URL.RequestURI(), http.StatusMovedPermanently)
		})
		// A "www." panel also answers on the apex and sends it to www.
		if apex := certs.Apex(domain); apex != strings.ToLower(domain) {
			next := srv.Handler
			srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.EqualFold(r.Host, apex) {
					redirect(w, r)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
		if cfg.TLS.Auto {
			m, err := certs.New(certs.Options{
				Dir: filepath.Join(cfg.DataDir, "certs"), Email: cfg.TLS.Email, Domain: domain,
				CloudflareToken: cfg.TLS.CloudflareToken, Staging: cfg.TLS.Staging, Log: log,
				Allow: func(ctx context.Context, host string) error {
					if web.SubLinks().AllowedTLSHost(ctx, host) {
						return nil
					}
					return fmt.Errorf("host %q is neither the panel nor a subscription host", host)
				},
			})
			if err != nil {
				return err
			}
			defer m.Stop()
			if err := m.Start(ctx); err != nil {
				return fmt.Errorf("tls: %w", err)
			}
			srv.TLSConfig = m.TLSConfig()
			// Port 80 answers HTTP-01 challenges and redirects everything else.
			httpSrv = &http.Server{Addr: cfg.TLS.HTTPListen, Handler: m.HTTPHandler(redirect), ReadHeaderTimeout: 10 * time.Second}
			log.Info("automatic certificates", "names", m.Managed(), "dns01", m.DNS(), "store", filepath.Join(cfg.DataDir, "certs"))
		} else {
			httpSrv = &http.Server{Addr: cfg.TLS.HTTPListen, Handler: redirect, ReadHeaderTimeout: 10 * time.Second}
		}
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("http listener", "addr", cfg.TLS.HTTPListen, "err", err)
			}
		}()
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		if httpSrv != nil {
			_ = httpSrv.Shutdown(shutdown)
		}
	}()
	log.Info("captain listening", "addr", cfg.Listen, "tls", cfg.TLSEnabled(), "base_url", cfg.BaseURL, "version", version)
	var err2 error
	if cfg.TLSEnabled() {
		err2 = srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key) // empty paths use TLSConfig's GetCertificate
	} else {
		err2 = srv.ListenAndServe()
	}
	if err2 != nil && err2 != http.ErrServerClosed {
		return err2
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
