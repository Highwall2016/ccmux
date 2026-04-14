package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ccmux/backend/internal/auth"
	"github.com/lib/pq"
)

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	userID, err := a.DB.CreateUser(req.Email, hash)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			http.Error(w, "email already registered", http.StatusConflict)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	a.issueTokensResponse(w, http.StatusCreated, userID, req.Email)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	user, err := a.DB.GetUserByEmail(req.Email)
	if err != nil || user == nil || auth.CheckPassword(req.Password, user.PasswordHash) != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	a.issueTokensResponse(w, http.StatusOK, user.ID, user.Email)
}

func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	stored, err := a.DB.GetRefreshToken(auth.HashRefreshToken(req.RefreshToken))
	if err != nil || stored == nil || stored.Revoked || stored.ExpiresAt.Before(time.Now()) {
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		return
	}
	user, err := a.DB.GetUserByID(stored.UserID)
	if err != nil || user == nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	accessToken, err := auth.NewAccessToken(user.ID, user.Email, a.JWTSecret)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"access_token": accessToken})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	_ = a.DB.RevokeRefreshToken(auth.HashRefreshToken(req.RefreshToken))
	w.WriteHeader(http.StatusNoContent)
}

// issueTokensResponse generates access + refresh tokens and writes them as JSON.
func (a *App) issueTokensResponse(w http.ResponseWriter, status int, userID, email string) {
	accessToken, err := auth.NewAccessToken(userID, email, a.JWTSecret)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rawRefresh, hashRefresh, err := auth.GenerateRefreshToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err = a.DB.CreateRefreshToken(userID, hashRefresh, time.Now().Add(30*24*time.Hour)); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, status, map[string]string{
		"access_token":  accessToken,
		"refresh_token": rawRefresh,
		"user_id":       userID,
	})
}
