// Package notify sends push notifications via FCM v1 HTTP API.
// Both iOS (through APNs via Firebase) and Android are handled by a single FCM sender,
// so no separate APNs client is needed on the backend.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const fcmEndpointFmt = "https://fcm.googleapis.com/v1/projects/%s/messages:send"

// ErrTokenInvalid is returned when FCM reports the token as unregistered.
// The caller should remove the token from the database.
var ErrTokenInvalid = errors.New("fcm: token invalid or unregistered")

// FCMSender sends push notifications via the FCM v1 HTTP API.
type FCMSender struct {
	projectID  string
	httpClient *http.Client // pre-configured with OAuth2 token source
}

// NewFCMSender creates an FCMSender using the service account JSON at keyPath.
// Returns nil if keyPath is empty so the server runs without push credentials.
func NewFCMSender(keyPath, projectID string) *FCMSender {
	if keyPath == "" || projectID == "" {
		log.Println("[notify] FCM not configured — push notifications disabled")
		return nil
	}

	data, err := os.ReadFile(keyPath)
	if err != nil {
		log.Printf("[notify] cannot read FCM key file %s: %v — push disabled", keyPath, err)
		return nil
	}

	ctx := context.Background()
	creds, err := google.CredentialsFromJSONWithParams(ctx, data, google.CredentialsParams{
		Scopes: []string{"https://www.googleapis.com/auth/firebase.messaging"},
	})
	if err != nil {
		log.Printf("[notify] FCM credentials error: %v — push disabled", err)
		return nil
	}

	return &FCMSender{
		projectID:  projectID,
		httpClient: oauth2.NewClient(ctx, creds.TokenSource),
	}
}

// Send delivers a push notification to a single FCM registration token.
// Returns ErrTokenInvalid if FCM reports the token as unregistered (caller should delete it).
func (s *FCMSender) Send(ctx context.Context, token, title, body string, data map[string]string) error {
	type notification struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	type android struct {
		Priority string `json:"priority"`
	}
	type apsInner struct {
		ContentAvailable int `json:"content-available"`
	}
	type apnsPayload struct {
		APS apsInner `json:"aps"`
	}
	type apns struct {
		Payload apnsPayload       `json:"payload"`
		Headers map[string]string `json:"headers"`
	}
	type messageInner struct {
		Token        string            `json:"token"`
		Notification notification      `json:"notification"`
		Data         map[string]string `json:"data,omitempty"`
		Android      android           `json:"android"`
		APNS         apns              `json:"apns"`
	}
	type request struct {
		Message messageInner `json:"message"`
	}

	req := request{Message: messageInner{
		Token:        token,
		Notification: notification{Title: title, Body: body},
		Data:         data,
		Android:      android{Priority: "high"},
		APNS: apns{
			Payload: apnsPayload{APS: apsInner{ContentAvailable: 1}},
			Headers: map[string]string{"apns-priority": "10"},
		},
	}}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("fcm marshal: %w", err)
	}

	url := fmt.Sprintf(fcmEndpointFmt, s.projectID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("fcm send: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound, http.StatusGone:
		return ErrTokenInvalid
	default:
		return fmt.Errorf("fcm unexpected status %d", resp.StatusCode)
	}
}
