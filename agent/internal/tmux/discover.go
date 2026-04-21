package tmux

import (
	"fmt"
	"log"
	"time"

	agentpty "github.com/ccmux/agent/internal/pty"
	"github.com/ccmux/agent/internal/relay"
	"github.com/ccmux/backend/pkg/protocol"
	"github.com/vmihailenco/msgpack/v5"
)

// DiscoverAndRegister performs an immediate reconcile of running tmux panes,
// registering each as a ccmux session via ptyMgr.SpawnTmux, and starts a
// background goroutine that repeats the reconcile every interval.
//
// If tmux is not running, the function returns immediately without error.
// The background goroutine also exits quietly when tmux stops.
func DiscoverAndRegister(
	deviceID string,
	ptyMgr *agentpty.Manager,
	wsConn *relay.Conn,
	interval time.Duration,
	announceFunc func(sessionID, name string, tmuxBacked bool, tmuxTarget string),
) {
	if !IsRunning() {
		return
	}
	reconcile(deviceID, ptyMgr, wsConn, announceFunc)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if !IsRunning() {
				continue
			}
			reconcile(deviceID, ptyMgr, wsConn, announceFunc)
		}
	}()
}

// reconcile syncs running tmux panes with registered ccmux sessions and
// broadcasts the current tmux topology to mobile clients.
func reconcile(
	deviceID string,
	ptyMgr *agentpty.Manager,
	wsConn *relay.Conn,
	announceFunc func(sessionID, name string, tmuxBacked bool, tmuxTarget string),
) {
	panes, err := ListPanes()
	if err != nil {
		log.Printf("[tmux] list-panes: %v", err)
		return
	}

	// Build a set of known pane IDs from the current tmux server.
	knownIDs := make(map[string]struct{}, len(panes))
	for _, p := range panes {
		knownIDs[p.CcmuxID] = struct{}{}
	}

	// Register any pane not yet tracked.
	for _, p := range panes {
		if ptyMgr.HasSession(p.CcmuxID) || ptyMgr.WasTargetKilled(p.Target) {
			continue
		}
		displayName := fmt.Sprintf("%s/%s", p.TmuxSession, p.WindowName)
		if err := ptyMgr.SpawnTmux(p.CcmuxID, displayName, p.Target, 80, 24); err != nil {
			log.Printf("[tmux] spawn pane %s (%s): %v", p.Target, p.CcmuxID[:8], err)
			continue
		}
		announceFunc(p.CcmuxID, displayName, true, p.Target)
		log.Printf("[tmux] registered pane %s as session %s (%s)", p.Target, p.CcmuxID[:8], displayName)
	}

	// Send the current topology to subscribed mobile clients.
	sendTmuxTree(deviceID, panes, wsConn)
}

// sendTmuxTree encodes the tmux pane hierarchy and sends it as a TypeTmuxTree
// packet to the backend, which forwards it to subscribed mobile clients.
func sendTmuxTree(deviceID string, panes []PaneInfo, wsConn *relay.Conn) {
	// Group panes by session → window.
	type windowKey struct {
		session     string
		windowIndex int
	}
	type windowMeta struct {
		name  string
		panes []protocol.TmuxPaneTree
	}
	windows := make(map[windowKey]*windowMeta)
	sessionOrder := []string{}
	seenSessions := map[string]struct{}{}

	for _, p := range panes {
		if _, ok := seenSessions[p.TmuxSession]; !ok {
			seenSessions[p.TmuxSession] = struct{}{}
			sessionOrder = append(sessionOrder, p.TmuxSession)
		}
		k := windowKey{p.TmuxSession, p.WindowIndex}
		if windows[k] == nil {
			windows[k] = &windowMeta{name: p.WindowName}
		}
		windows[k].panes = append(windows[k].panes, protocol.TmuxPaneTree{
			Index:   p.PaneIndex,
			CcmuxID: p.CcmuxID,
			Title:   p.Title,
			Active:  p.Target == fmt.Sprintf("%s:%d.%d", p.TmuxSession, p.WindowIndex, p.PaneIndex),
		})
	}

	// Build session trees in order.
	sessionTrees := make([]protocol.TmuxSessionTree, 0, len(sessionOrder))
	for _, sessName := range sessionOrder {
		// Collect windows for this session, sorted by window index.
		var wins []protocol.TmuxWindowTree
		for wi := 0; wi < 100; wi++ { // window indices rarely exceed 20
			k := windowKey{sessName, wi}
			wm, ok := windows[k]
			if !ok {
				continue
			}
			wins = append(wins, protocol.TmuxWindowTree{
				Index: wi,
				Name:  wm.name,
				Panes: wm.panes,
			})
		}
		if len(wins) > 0 {
			sessionTrees = append(sessionTrees, protocol.TmuxSessionTree{
				Name:    sessName,
				Windows: wins,
			})
		}
	}

	tree := protocol.TmuxTreePayload{
		DeviceID: deviceID,
		Sessions: sessionTrees,
	}
	payload, err := msgpack.Marshal(&tree)
	if err != nil {
		return
	}
	pkt, err := (&protocol.Packet{
		Type:    protocol.TypeTmuxTree,
		Payload: payload,
	}).Encode()
	if err != nil {
		return
	}
	wsConn.Send(pkt)
}
