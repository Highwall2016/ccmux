module github.com/ccmux/agent

go 1.25.2

require (
	github.com/ccmux/backend v0.0.0
	github.com/creack/pty v1.1.21
	github.com/gorilla/websocket v1.5.3
	github.com/vmihailenco/msgpack/v5 v5.4.1
	golang.org/x/term v0.25.0
)

require (
	github.com/shirou/gopsutil/v3 v3.24.5 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
)

replace github.com/ccmux/backend => ../backend
