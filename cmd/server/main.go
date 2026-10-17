// Command server runs the merch web app: JSON API + PWA static files.
// All configuration comes from environment variables (see .env.example).
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/authors"
	"github.com/hackclub/merch/internal/cdn"
	"github.com/hackclub/merch/internal/config"
	"github.com/hackclub/merch/internal/db"
	"github.com/hackclub/merch/internal/httpapi"
	"github.com/hackclub/merch/internal/loops"
	"github.com/hackclub/merch/internal/places"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
	"github.com/hackclub/merch/internal/theseus"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	var dir authors.Directory = authors.Static{}
	if cfg.AirtableAPIKey != "" {
		dir = authors.NewAirtable(authors.AirtableConfig{
			APIKey: cfg.AirtableAPIKey, BaseID: cfg.AirtableAuthorsBaseID,
			Table: cfg.AirtableAuthorsTable, EmailField: cfg.AirtableAuthorsEmailFld,
		})
	} else {
		log.Warn("AIRTABLE_API_KEY not set: nobody will be recognized as a YSWS author (admins still are)")
	}

	var images httpapi.ImageStore
	if cfg.CDNAPIKey != "" {
		images = cdn.New(cfg.CDNBaseURL, cfg.CDNAPIKey, nil)
	} else {
		log.Warn("HACKCLUB_CDN_API_KEY not set: admins can't upload item images")
	}

	var finder httpapi.AddressFinder
	if cfg.GooglePlacesAPIKey != "" {
		finder = places.New(cfg.GooglePlacesBaseURL, cfg.GooglePlacesAPIKey, nil)
	} else {
		log.Warn("GOOGLE_PLACES_API_KEY not set: the request form has no address autocomplete")
	}

	var payments httpapi.Payments
	if cfg.StripeSecretKey != "" && cfg.StripePublishableKey != "" {
		payments = stripe.New(cfg.StripeBaseURL, cfg.StripeSecretKey, nil)
		if strings.HasPrefix(cfg.StripeSecretKey, "sk_test_") {
			log.Warn("STRIPE_SECRET_KEY is a sandbox key: card payments are test payments")
		}
	} else {
		log.Warn("STRIPE_SECRET_KEY and STRIPE_PUBLISHABLE_KEY not both set: international shipping is paid on HCB (HCB_SHIPPING_PAYMENT_URL)")
	}

	var mailer httpapi.Mailer
	if cfg.LoopsAPIKey != "" {
		mailer = loops.New(cfg.LoopsBaseURL, cfg.LoopsAPIKey, cfg.LoopsTransactionalID, nil)
	} else {
		log.Warn("LOOPS_API_KEY not set: requesters get no emails")
	}

	var static fs.FS
	if cfg.StaticDir != "" {
		if st, err := os.Stat(cfg.StaticDir); err == nil && st.IsDir() {
			static = os.DirFS(cfg.StaticDir)
		} else {
			log.Warn("static dir missing; serving API only (run `make web`)", "dir", cfg.StaticDir)
		}
	}

	h := httpapi.New(httpapi.Deps{
		Config: cfg,
		Store:  store.New(pool),
		Provider: auth.NewHCA(auth.HCAConfig{
			BaseURL: cfg.HCABaseURL, ClientID: cfg.HCAClientID, ClientSecret: cfg.HCAClientSecret,
			Scopes: cfg.HCAScopes,
		}),
		Authors:   dir,
		Warehouse: theseus.New(cfg.TheseusBaseURL, cfg.TheseusAPIKey, nil),
		Images:    images,
		Places:    finder,
		Payments:  payments,
		Mailer:    mailer,
		Static:    static,
		Log:       log,
		Warm:      true,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "origins", cfg.Origins())
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
