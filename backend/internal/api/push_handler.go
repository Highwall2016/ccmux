package api

import (
	"encoding/json"
	"net/http"

	mw "github.com/ccmux/backend/internal/api/middleware"
)

func (a *App) handleRegisterPushToken(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	var req struct {
		Platform   string `json:"platform"`    // ios | android
		Token      string `json:"token"`
		DeviceName string `json:"device_name"` // optional, e.g. "iPhone 16 Pro"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Platform == "" || req.Token == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := a.DB.UpsertPushToken(userID, req.Platform, req.Token, req.DeviceName); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleDeletePushToken(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := a.DB.DeletePushToken(userID, req.Token); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
