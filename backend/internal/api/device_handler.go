package api

import (
	"encoding/json"
	"net/http"
	"time"

	mw "github.com/ccmux/backend/internal/api/middleware"
	"github.com/ccmux/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

func (a *App) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	var req struct {
		Name     string `json:"name"`
		Platform string `json:"platform"` // macos | linux | windows
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Platform == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	rawToken, tokenHash, err := auth.GenerateDeviceToken(a.HMACSecret)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	deviceID, err := a.DB.CreateDevice(userID, req.Name, req.Platform, tokenHash)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"device_id":    deviceID,
		"device_token": rawToken, // returned only once — agent must save this
	})
}

func (a *App) handleListDevices(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	devices, err := a.DB.ListDevicesByUser(userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	type deviceResp struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		Platform  string  `json:"platform"`
		LastSeen  *string `json:"last_seen,omitempty"`
		CreatedAt string  `json:"created_at"`
	}
	resp := make([]deviceResp, 0, len(devices))
	for _, d := range devices {
		dr := deviceResp{
			ID:        d.ID,
			Name:      d.Name,
			Platform:  d.Platform,
			CreatedAt: d.CreatedAt.Format(time.RFC3339),
		}
		if d.LastSeen != nil {
			s := d.LastSeen.Format(time.RFC3339)
			dr.LastSeen = &s
		}
		resp = append(resp, dr)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	deviceID := chi.URLParam(r, "deviceID")
	if err := a.DB.DeleteDevice(deviceID, userID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
