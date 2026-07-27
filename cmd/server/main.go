package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	labweb "github.com/telemetry-sh/goroutine-leak-lab/internal/web"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           labweb.New(logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("goroutine leak lab listening", "url", fmt.Sprintf("http://localhost:%s", port))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
