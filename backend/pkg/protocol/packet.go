package protocol

import "github.com/vmihailenco/msgpack/v5"

// Packet type constants — wire protocol over WebSocket binary frames.
const (
	TypeTerminalOutput = uint8(0x01)
	TypeTerminalInput  = uint8(0x02)
	TypeResize         = uint8(0x03)
	TypeSessionList    = uint8(0x10)
	TypeSessionStatus  = uint8(0x11)
	TypeScrollback     = uint8(0x12)
	TypeScrollbackDone = uint8(0x13)
	TypeAlert          = uint8(0x14)
	TypeKillSession    = uint8(0x15) // backend → agent: kill the session
	TypeRenameSession  = uint8(0x16) // agent → backend: rename the session
	TypeAuth           = uint8(0x20)
	TypeAuthOK         = uint8(0x21)
	TypeAuthFail       = uint8(0x22)
	TypeSubscribe      = uint8(0x30)
	TypeUnsubscribe    = uint8(0x31)
	TypePing           = uint8(0xFF)
	TypePong           = uint8(0xFE)
)

// Packet is the wire format for all WebSocket messages.
type Packet struct {
	Type    uint8  `msgpack:"t"`
	Session string `msgpack:"s,omitempty"`
	Payload []byte `msgpack:"p,omitempty"`
}

// ResizePayload is used with TypeResize.
type ResizePayload struct {
	Cols uint16 `msgpack:"c"`
	Rows uint16 `msgpack:"r"`
}

// SessionStatusPayload is used with TypeSessionStatus.
type SessionStatusPayload struct {
	SessionID string `msgpack:"id"`
	Status    string `msgpack:"status"` // active | exited | killed
	ExitCode  *int   `msgpack:"exit_code,omitempty"`
	Name      string `msgpack:"name,omitempty"`
	Command   string `msgpack:"cmd,omitempty"`
}

// RenamePayload is used with TypeRenameSession.
type RenamePayload struct {
	SessionID string `msgpack:"id"`
	Name      string `msgpack:"name"`
}

// AlertPayload is used with TypeAlert.
type AlertPayload struct {
	Pattern string `msgpack:"p"`
	Excerpt []byte `msgpack:"e"`
}

// SubscribePayload is used with TypeSubscribe.
type SubscribePayload struct {
	SessionID  string `msgpack:"id"`
	FromOffset string `msgpack:"offset,omitempty"` // Redis Streams offset
}

// Encode serialises a Packet to MessagePack bytes.
func (pkt *Packet) Encode() ([]byte, error) {
	return msgpack.Marshal(pkt)
}

// Decode deserialises MessagePack bytes into a Packet.
func Decode(data []byte) (*Packet, error) {
	var pkt Packet
	if err := msgpack.Unmarshal(data, &pkt); err != nil {
		return nil, err
	}
	return &pkt, nil
}
