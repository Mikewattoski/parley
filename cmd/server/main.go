package main

import (
	"context"
	_ "embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/Mikewattoski/parley/internal/auth"
	"github.com/Mikewattoski/parley/internal/hub"
	"github.com/Mikewattoski/parley/internal/server"
	"github.com/Mikewattoski/parley/internal/store"
)

//go:embed index.html
var indexHTML []byte

func main() {
	addr := getenv("ADDR", ":8081")
	origins := splitCSV(getenv("ALLOWED_ORIGINS", "http://localhost:5173"))
	dbURL := getenv("DATABASE_URL", "postgres://parley:parley_pass@localhost:5433/parley?sslmode=disable")

	tokens, err := auth.NewService(os.Getenv("JWT_SECRET"))
	if err != nil {
		log.Fatalf("JWT_SECRET: %v (generate one with: openssl rand -hex 32)", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		log.Fatalf("db ping: %v (is it running? try: make up)", err)
	}
	cancel()

	db := store.New(pool)
	h := hub.New().WithStore(db)
	go h.Run()

	srv := &http.Server{
		Addr: addr,
		Handler: server.New(server.Config{
			Hub:       h,
			Store:     db,
			Auth:      tokens,
			Origins:   origins,
			Index:     indexHTML,
			AuthRate:  rate.Every(6 * time.Second), // then 10 attempts per minute per IP
			AuthBurst: 10,
		}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("parley listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx) // stops new connections (hijacked sockets are not tracked)

	h.Stop() // sends a close frame to every connected client
	<-h.Done()
	time.Sleep(300 * time.Millisecond) // let the close frames flush
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
