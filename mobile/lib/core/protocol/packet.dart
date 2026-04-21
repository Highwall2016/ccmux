import 'dart:typed_data';
import 'package:messagepack/messagepack.dart';

// Packet type constants — must exactly match backend/pkg/protocol/packet.go.
const int typeTerminalOutput = 0x01;
const int typeTerminalInput = 0x02;
const int typeResize = 0x03;
const int typeSessionList = 0x10;
const int typeSessionStatus = 0x11;
const int typeScrollback = 0x12;
const int typeScrollbackDone = 0x13;
const int typeAlert = 0x14;
const int typeAuth = 0x20;
const int typeAuthOK = 0x21;
const int typeAuthFail = 0x22;
const int typeSubscribe = 0x30;
const int typeUnsubscribe = 0x31;
const int typeTmuxTree = 0x32; // agent → backend → clients: tmux pane hierarchy
const int typePing = 0xFF;
const int typePong = 0xFE;

/// Wire packet matching the Go [Packet] struct with msgpack tags t/s/p.
class Packet {
  final int type;
  final String? session;
  final Uint8List? payload;

  const Packet({required this.type, this.session, this.payload});

  /// Encode to MessagePack binary, omitting null fields.
  Uint8List encode() {
    int fieldCount = 1; // always "t"
    if (session != null && session!.isNotEmpty) fieldCount++;
    if (payload != null && payload!.isNotEmpty) fieldCount++;

    final p = Packer();
    p.packMapLength(fieldCount);

    p.packString('t');
    p.packInt(type);

    if (session != null && session!.isNotEmpty) {
      p.packString('s');
      p.packString(session!);
    }
    if (payload != null && payload!.isNotEmpty) {
      p.packString('p');
      p.packBinary(payload!);
    }

    return p.takeBytes();
  }

  /// Decode a MessagePack binary frame into a Packet.
  static Packet decode(Uint8List data) {
    final u = Unpacker.fromList(data);
    final int mapLen = u.unpackMapLength();

    int type = 0;
    String? session;
    Uint8List? payload;

    for (int i = 0; i < mapLen; i++) {
      final key = u.unpackString()!;
      switch (key) {
        case 't':
          type = u.unpackInt()!;
        case 's':
          session = u.unpackString();
        case 'p':
          final binary = u.unpackBinary();
          payload = Uint8List.fromList(binary);
        default:
          u.unpackString(); // consume unknown fields
      }
    }
    return Packet(type: type, session: session, payload: payload);
  }
}

/// Encode a [ResizePayload] — matches protocol.ResizePayload{Cols, Rows}.
Uint8List encodeResizePayload(int cols, int rows) {
  final p = Packer();
  p.packMapLength(2);
  p.packString('c');
  p.packInt(cols);
  p.packString('r');
  p.packInt(rows);
  return p.takeBytes();
}

/// Encode a [SubscribePayload] — matches protocol.SubscribePayload{SessionID, FromOffset}.
Uint8List encodeSubscribePayload(String sessionId, {String fromOffset = ''}) {
  final int fieldCount = fromOffset.isNotEmpty ? 2 : 1;
  final p = Packer();
  p.packMapLength(fieldCount);
  p.packString('id');
  p.packString(sessionId);
  if (fromOffset.isNotEmpty) {
    p.packString('offset');
    p.packString(fromOffset);
  }
  return p.takeBytes();
}
