package api

import (
	"encoding/json"
	"net/http"

	mw "github.com/ccmux/backend/internal/api/middleware"
	"github.com/ccmux/backend/internal/hub"
	"github.com/ccmux/backend/internal/notify"
	"github.com/ccmux/backend/internal/store"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

// App holds shared dependencies for all HTTP handlers.
type App struct {
	DB         *store.DB
	Hub        *hub.Hub
	Notify     *notify.Dispatcher // nil when push notifications are not configured
	JWTSecret  string
	HMACSecret string
}

// NewRouter builds and returns the application HTTP router.
func (a *App) NewRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(mw.RateLimiter(rate.Limit(20), 60)) // 20 req/s, burst 60

	r.Route("/api", func(r chi.Router) {
		// Auth endpoints — stricter rate limit to slow brute-force.
		r.Group(func(r chi.Router) {
			r.Use(mw.RateLimiter(rate.Limit(0.2), 5)) // ~12/min, burst 5
			r.Post("/auth/register", a.handleRegister)
			r.Post("/auth/login", a.handleLogin)
			r.Post("/auth/refresh", a.handleRefresh)
		})

		// Authenticated endpoints.
		r.Group(func(r chi.Router) {
			r.Use(mw.Auth(a.JWTSecret))
			r.Post("/auth/logout", a.handleLogout)

			r.Get("/devices", a.handleListDevices)
			r.Post("/devices", a.handleRegisterDevice)
			r.Delete("/devices/{deviceID}", a.handleDeleteDevice)
			r.Get("/devices/{deviceID}/sessions", a.handleListSessions)
			r.Patch("/devices/{deviceID}/sessions/{sessionID}", a.handleRenameSession)
			r.Delete("/devices/{deviceID}/sessions/{sessionID}", a.handleKillSession)

			r.Post("/push/register", a.handleRegisterPushToken)
			r.Delete("/push/register", a.handleDeletePushToken)
		})
	})

	// WebSocket endpoints — auth is performed in-protocol after upgrade.
	r.Get("/ws/agent", a.handleAgentWS)
	r.Get("/ws/client", a.handleClientWS)

	return r
}

// writeJSON writes v as JSON with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
