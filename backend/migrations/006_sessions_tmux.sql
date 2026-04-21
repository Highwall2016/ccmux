-- Add tmux integration columns to terminal_sessions.
-- tmux_target: the tmux pane target string (e.g. "main:1.0") when the session
--              is backed by a running tmux pane.
-- tmux_backed: true when the ccmux session wraps a tmux pane.  Used to skip
--              sessions during MarkDeviceSessionsExited so that tmux-backed
--              sessions survive agent restarts.
ALTER TABLE terminal_sessions
  ADD COLUMN IF NOT EXISTS tmux_target TEXT,
  ADD COLUMN IF NOT EXISTS tmux_backed BOOLEAN NOT NULL DEFAULT FALSE;
