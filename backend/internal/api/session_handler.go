package api

import (
	"net/http"
	"time"

	mw "github.com/ccmux/backend/internal/api/middleware"
	"github.com/go-chi/chi/v5"
)

func (a *App) handleListSessions(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	deviceID := chi.URLParam(r, "deviceID")

	// Verify device belongs to the requesting user.
	device, err := a.DB.GetDeviceByID(deviceID)
	if err != nil || device == nil || device.UserID != userID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	sessions, err := a.DB.ListSessionsByDevice(deviceID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	type sessionResp struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Command      string `json:"command"`
		Status       string `json:"status"`
		ExitCode     *int   `json:"exit_code,omitempty"`
		StartedAt    string `json:"started_at"`
		LastActivity string `json:"last_activity"`
	}
	resp := make([]sessionResp, 0, len(sessions))
	for _, s := range sessions {
		resp = append(resp, sessionResp{
			ID:           s.ID,
			Name:         s.Name,
			Command:      s.Command,
			Status:       s.Status,
			ExitCode:     s.ExitCode,
			StartedAt:    s.StartedAt.Format(time.RFC3339),
			LastActivity: s.LastActivity.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
