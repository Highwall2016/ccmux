package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ccmux/backend/internal/store"
)

// Dispatcher fans out push notifications to all push tokens belonging to a user.
type Dispatcher struct {
	db  *store.DB
	fcm *FCMSender // nil if FCM is not configured
}

// NewDispatcher creates a Dispatcher. fcm may be nil (notifications are silently skipped).
func NewDispatcher(db *store.DB, fcm *FCMSender) *Dispatcher {
	return &Dispatcher{db: db, fcm: fcm}
}

// NotifySessionExit sends a push notification when a PTY session exits.
func (d *Dispatcher) NotifySessionExit(userID, sessionName, command string, exitCode int) {
	if d.fcm == nil {
		return
	}
	title := fmt.Sprintf("Session ended: %s", sessionName)
	body := fmt.Sprintf("%q exited with code %d", command, exitCode)
	data := map[string]string{"type": "session_exit", "session_name": sessionName}
	d.fanOut(userID, title, body, data)
}

// NotifyAlert sends a push notification when a watch pattern matches PTY output.
func (d *Dispatcher) NotifyAlert(userID, sessionID, sessionName string, excerpt []byte) {
	if d.fcm == nil {
		return
	}
	title := fmt.Sprintf("Alert in %s", sessionName)
	body := truncate(string(excerpt), 100)
	data := map[string]string{
		"type":       "alert",
		"session_id": sessionID,
	}
	d.fanOut(userID, title, body, data)
}

// NotifyDeviceOnline sends a push notification when a device agent connects.
func (d *Dispatcher) NotifyDeviceOnline(userID, deviceName string) {
	if d.fcm == nil {
		return
	}
	title := "Device online"
	body := fmt.Sprintf("%s is now connected", deviceName)
	data := map[string]string{"type": "device_online", "device_name": deviceName}
	d.fanOut(userID, title, body, data)
}

// fanOut delivers a notification to every push token registered for userID.
func (d *Dispatcher) fanOut(userID, title, body string, data map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tokens, err := d.db.GetPushTokensForUser(userID)
	if err != nil {
		log.Printf("[notify] get push tokens for user %s: %v", userID, err)
		return
	}

	for _, t := range tokens {
		tok := t // capture loop var
		go func() {
			if err := d.fcm.Send(ctx, tok.Token, title, body, data); err != nil {
				if errors.Is(err, ErrTokenInvalid) {
					log.Printf("[notify] deleting stale push token for user %s", userID)
					_ = d.db.DeleteExpiredPushToken(tok.Token)
				} else {
					log.Printf("[notify] FCM send error for user %s: %v", userID, err)
				}
			}
		}()
	}
}

// truncate clips s to at most n bytes (whole ASCII characters only).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
