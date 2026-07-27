package web

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/telemetry-sh/goroutine-leak-lab/internal/sim"
)

//go:embed assets/*
var assets embed.FS

type Server struct {
	logger *slog.Logger
	mux    *http.ServeMux
}

func New(logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{logger: logger, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mux.ServeHTTP(writer, request)
}

func (server *Server) routes() {
	static, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}

	server.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(static))))
	server.mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = writer.Write([]byte("ok\n"))
	})
	server.mux.HandleFunc("GET /api/simulate", server.simulateDefault)
	server.mux.HandleFunc("POST /api/simulate", server.simulate)
	server.mux.HandleFunc("GET /", server.index)
}

func (server *Server) index(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	body, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(writer, "interface unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	_, _ = writer.Write(body)
}

func (server *Server) simulateDefault(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, sim.Run(sim.DefaultConfig()))
}

func (server *Server) simulate(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, 32<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var config sim.Config
	if err := decoder.Decode(&config); err != nil {
		server.logger.Warn("invalid simulation request", "error", err)
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid simulation configuration"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}

	writeJSON(writer, http.StatusOK, sim.Run(config))
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}
