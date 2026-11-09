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

	"github.com/Mikewattoski/parley/internal/hub"
	"github.com/Mikewattoski/parley/internal/server"
)

//go:embed index.html
var indexHTML []byte

func main() {
	addr := getenv("ADDR", ":8081")
	origins := splitCSV(getenv("ALLOWED_ORIGINS", "http://localhost:5173"))

	h := hub.New()
	go h.Run()

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(h, origins, indexHTML).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("parley listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
