package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/telemetry-sh/goroutine-leak-lab/internal/sim"
)

func TestHomeAndHealth(t *testing.T) {
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)))

	home := httptest.NewRecorder()
	server.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if home.Code != http.StatusOK {
		t.Fatalf("home status = %d", home.Code)
	}
	if !strings.Contains(home.Body.String(), "THE REQUEST RETURNED") {
		t.Fatal("home page does not contain the lab headline")
	}

	health := httptest.NewRecorder()
	server.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok\n" {
		t.Fatalf("unexpected health response: %d %q", health.Code, health.Body.String())
	}
}

func TestSimulationAPI(t *testing.T) {
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/simulate", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var result sim.Response
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Strategies) != 4 {
		t.Fatalf("got %d strategies, want 4", len(result.Strategies))
	}
}

func TestSimulationPOSTNormalizesInput(t *testing.T) {
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"requestsPerSecond":9999,"timeoutMs":120,"slowWorkPercent":18,"slowWorkMs":1600,"fastWorkMs":45,"poolSize":24,"queueSize":80,"runSeconds":10,"seed":12}`
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/simulate", strings.NewReader(body)))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result sim.Response
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Config.RequestsPerSecond != 500 {
		t.Fatalf("requests per second = %d, want normalized maximum", result.Config.RequestsPerSecond)
	}
}

func TestSimulationRejectsUnknownFields(t *testing.T) {
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/simulate", strings.NewReader(`{"surprise":true}`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
