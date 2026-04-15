package api

import (
	"encoding/json"
	"net/http"
	"time"

	mw "github.com/ccmux/backend/internal/api/middleware"
	"github.com/ccmux/backend/pkg/protocol"
	"github.com/go-chi/chi/v5"
	"github.com/vmihailenco/msgpack/v5"
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

// handleRenameSession handles PATCH /api/devices/{deviceID}/sessions/{sessionID}.
func (a *App) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	deviceID := chi.URLParam(r, "deviceID")
	sessionID := chi.URLParam(r, "sessionID")

	device, err := a.DB.GetDeviceByID(deviceID)
	if err != nil || device == nil || device.UserID != userID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	session, err := a.DB.GetSessionByID(sessionID)
	if err != nil || session == nil || session.DeviceID != deviceID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if err := a.DB.RenameSession(sessionID, body.Name); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Broadcast the rename as a TypeSessionStatus "active" packet so connected
	// clients update their UI without a full refresh.
	sp := protocol.SessionStatusPayload{
		SessionID: sessionID,
		Status:    session.Status,
		Name:      body.Name,
	}
	if payload, err := msgpack.Marshal(&sp); err == nil {
		if pkt, err := (&protocol.Packet{
			Type:    protocol.TypeSessionStatus,
			Session: sessionID,
			Payload: payload,
		}).Encode(); err == nil {
			a.Hub.Broadcast(sessionID, pkt)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleKillSession handles DELETE /api/devices/{deviceID}/sessions/{sessionID}.
// It marks the session as killed in the DB and forwards a TypeKillSession packet
// to the desktop agent so it terminates the local PTY.
func (a *App) handleKillSession(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	deviceID := chi.URLParam(r, "deviceID")
	sessionID := chi.URLParam(r, "sessionID")

	device, err := a.DB.GetDeviceByID(deviceID)
	if err != nil || device == nil || device.UserID != userID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	session, err := a.DB.GetSessionByID(sessionID)
	if err != nil || session == nil || session.DeviceID != deviceID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Send kill packet to the agent (best-effort).
	if agentConn := a.Hub.GetAgentConn(deviceID); agentConn != nil {
		if pkt, err := (&protocol.Packet{
			Type:    protocol.TypeKillSession,
			Session: sessionID,
		}).Encode(); err == nil {
			agentConn.Send(pkt)
		}
	}

	// Update the DB immediately so REST clients see the correct status without
	// waiting for the agent to confirm exit.
	_ = a.DB.UpdateStatus(sessionID, "killed", nil)

	// Broadcast a TypeSessionStatus "killed" packet so any subscribed WS
	// clients (e.g. the mobile terminal view) update their UI instantly.
	sp := protocol.SessionStatusPayload{
		SessionID: sessionID,
		Status:    "killed",
	}
	if payload, err := msgpack.Marshal(&sp); err == nil {
		if pkt, err := (&protocol.Packet{
			Type:    protocol.TypeSessionStatus,
			Session: sessionID,
			Payload: payload,
		}).Encode(); err == nil {
			a.Hub.Broadcast(sessionID, pkt)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
