// Package binary provides fixed-width binary encode/decode for all internal
// analytics events. It is the single source of truth for wire layouts —
// nothing else in the codebase may invent its own binary format.
//
// # Design goals
//
//   - Zero allocations on the decode path — caller owns the buffer.
//   - No external dependencies — stdlib encoding/binary + math only.
//   - Self-describing NATS frames so the consumer never needs out-of-band schema.
//
// # Redis sorted-set members
//
// The ZAdd score carries the unix timestamp. The member carries only the
// payload values — timestamp is NOT repeated to keep stored bytes minimal.
//
//	Scalar  (SMA / EMA / RSI)  –  8 bytes  : float64 value
//	OHLC                       – 32 bytes  : float64 × 4  (open, high, low, close)
//	BB                         – 24 bytes  : float64 × 3  (upper, middle, lower)
//	MACD                       – 24 bytes  : float64 × 3  (macd_line, signal_line, histogram)
//
// # NATS analytics frame layout
//
//	Offset  Size  Field
//	     0     1  type tag        (TypeSMA … TypeMACD)
//	     1     4  instrument_id   int32  big-endian
//	     5     1  resolution tag  (Res1s | Res1m)
//	     6     8  timestamp       int64  big-endian  (nanoseconds)
//	    14     N  payload         (same layout as Redis member)
//
// Total frame sizes: scalar=22 B, OHLC=46 B, BB=38 B, MACD=38 B.
package binary

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ---- type tags ----

const (
	TypeSMA  byte = 0x01
	TypeEMA  byte = 0x02
	TypeRSI  byte = 0x03
	TypeOHLC byte = 0x04
	TypeBB   byte = 0x05
	TypeMACD byte = 0x06
)

// ---- resolution tags ----

const (
	Res1s byte = 0x01
	Res1m byte = 0x02
)

// ---- frame header offsets ----

const (
	offType       = 0
	offInstrument = 1  // 4 bytes  int32
	offResolution = 5  // 1 byte
	offTimestamp  = 6  // 8 bytes  int64
	offPayload    = 14 // payload starts here
)

// ---- payload / frame sizes ----

const (
	SizeScalar = 8  // 1 × float64
	SizeOHLC   = 32 // 4 × float64
	SizeBB     = 24 // 3 × float64
	SizeMACD   = 24 // 3 × float64

	FrameScalar = offPayload + SizeScalar // 22
	FrameOHLC   = offPayload + SizeOHLC   // 46
	FrameBB     = offPayload + SizeBB     // 38
	FrameMACD   = offPayload + SizeMACD   // 38
)

// ============================================================
// Low-level helpers
// ============================================================

func putF64(b []byte, off int, v float64) {
	binary.BigEndian.PutUint64(b[off:], math.Float64bits(v))
}

func getF64(b []byte, off int) float64 {
	return math.Float64frombits(binary.BigEndian.Uint64(b[off:]))
}

func putI32(b []byte, off int, v int32) {
	binary.BigEndian.PutUint32(b[off:], uint32(v))
}

func getI32(b []byte, off int) int32 {
	return int32(binary.BigEndian.Uint32(b[off:]))
}

func putI64(b []byte, off int, v int64) {
	binary.BigEndian.PutUint64(b[off:], uint64(v))
}

func getI64(b []byte, off int) int64 {
	return int64(binary.BigEndian.Uint64(b[off:]))
}

// ResTag converts a resolution string to its 1-byte tag.
func ResTag(resolution string) byte {
	if resolution == "1m" {
		return Res1m
	}
	return Res1s
}

// ResString converts a resolution tag back to the canonical string.
func ResString(tag byte) string {
	if tag == Res1m {
		return "1m"
	}
	return "1s"
}

// ============================================================
// Redis member encode/decode — Scalar (SMA / EMA / RSI)
// ============================================================

// EncodeScalar encodes a single float64 value into an 8-byte Redis member.
func EncodeScalar(value float64) []byte {
	b := make([]byte, SizeScalar)
	putF64(b, 0, value)
	return b
}

// DecodeScalar extracts the float64 from an 8-byte Redis member.
func DecodeScalar(b []byte) (float64, error) {
	if len(b) < SizeScalar {
		return 0, fmt.Errorf("binary.DecodeScalar: need %d bytes, got %d", SizeScalar, len(b))
	}
	return getF64(b, 0), nil
}

// ============================================================
// Redis member encode/decode — OHLC
// ============================================================

// EncodeOHLC packs open/high/low/close into a 32-byte Redis member.
func EncodeOHLC(open, high, low, close float64) []byte {
	b := make([]byte, SizeOHLC)
	putF64(b, 0, open)
	putF64(b, 8, high)
	putF64(b, 16, low)
	putF64(b, 24, close)
	return b
}

// DecodeOHLC unpacks a 32-byte Redis member into open/high/low/close.
func DecodeOHLC(b []byte) (open, high, low, close float64, err error) {
	if len(b) < SizeOHLC {
		return 0, 0, 0, 0, fmt.Errorf("binary.DecodeOHLC: need %d bytes, got %d", SizeOHLC, len(b))
	}
	return getF64(b, 0), getF64(b, 8), getF64(b, 16), getF64(b, 24), nil
}

// ============================================================
// Redis member encode/decode — BB
// ============================================================

// EncodeBB packs upper/middle/lower into a 24-byte Redis member.
func EncodeBB(upper, middle, lower float64) []byte {
	b := make([]byte, SizeBB)
	putF64(b, 0, upper)
	putF64(b, 8, middle)
	putF64(b, 16, lower)
	return b
}

// DecodeBB unpacks a 24-byte Redis member into upper/middle/lower.
func DecodeBB(b []byte) (upper, middle, lower float64, err error) {
	if len(b) < SizeBB {
		return 0, 0, 0, fmt.Errorf("binary.DecodeBB: need %d bytes, got %d", SizeBB, len(b))
	}
	return getF64(b, 0), getF64(b, 8), getF64(b, 16), nil
}

// ============================================================
// Redis member encode/decode — MACD
// ============================================================

// EncodeMACD packs macdLine/signalLine/histogram into a 24-byte Redis member.
func EncodeMACD(macdLine, signalLine, histogram float64) []byte {
	b := make([]byte, SizeMACD)
	putF64(b, 0, macdLine)
	putF64(b, 8, signalLine)
	putF64(b, 16, histogram)
	return b
}

// DecodeMACD unpacks a 24-byte Redis member into macdLine/signalLine/histogram.
func DecodeMACD(b []byte) (macdLine, signalLine, histogram float64, err error) {
	if len(b) < SizeMACD {
		return 0, 0, 0, fmt.Errorf("binary.DecodeMACD: need %d bytes, got %d", SizeMACD, len(b))
	}
	return getF64(b, 0), getF64(b, 8), getF64(b, 16), nil
}

// ============================================================
// NATS frame encode/decode
// ============================================================

// Frame is the decoded representation of a NATS analytics frame.
type Frame struct {
	TypeTag      byte
	InstrumentID int32
	Resolution   byte   // Res1s or Res1m
	Timestamp    int64  // nanoseconds
	Payload      []byte // points into the original slice — do not modify
}

func writeHeader(b []byte, tag byte, instrumentID int32, resolution byte, timestamp int64) {
	b[offType] = tag
	putI32(b, offInstrument, instrumentID)
	b[offResolution] = resolution
	putI64(b, offTimestamp, timestamp)
}

// EncodeScalarFrame builds a complete NATS frame for SMA, EMA, or RSI.
func EncodeScalarFrame(typeTag byte, instrumentID int, resolution string, timestamp int64, value float64) []byte {
	b := make([]byte, FrameScalar)
	writeHeader(b, typeTag, int32(instrumentID), ResTag(resolution), timestamp)
	putF64(b, offPayload, value)
	return b
}

// EncodeOHLCFrame builds a complete NATS frame for OHLC.
func EncodeOHLCFrame(instrumentID int, resolution string, timestamp int64, open, high, low, closep float64) []byte {
	b := make([]byte, FrameOHLC)
	writeHeader(b, TypeOHLC, int32(instrumentID), ResTag(resolution), timestamp)
	putF64(b, offPayload+0, open)
	putF64(b, offPayload+8, high)
	putF64(b, offPayload+16, low)
	putF64(b, offPayload+24, closep)
	return b
}

// EncodeBBFrame builds a complete NATS frame for Bollinger Bands.
func EncodeBBFrame(instrumentID int, resolution string, timestamp int64, upper, middle, lower float64) []byte {
	b := make([]byte, FrameBB)
	writeHeader(b, TypeBB, int32(instrumentID), ResTag(resolution), timestamp)
	putF64(b, offPayload+0, upper)
	putF64(b, offPayload+8, middle)
	putF64(b, offPayload+16, lower)
	return b
}

// EncodeMACDFrame builds a complete NATS frame for MACD.
func EncodeMACDFrame(instrumentID int, resolution string, timestamp int64, macdLine, signalLine, histogram float64) []byte {
	b := make([]byte, FrameMACD)
	writeHeader(b, TypeMACD, int32(instrumentID), ResTag(resolution), timestamp)
	putF64(b, offPayload+0, macdLine)
	putF64(b, offPayload+8, signalLine)
	putF64(b, offPayload+16, histogram)
	return b
}

// DecodeFrame parses the header and returns a Frame. Payload points into b
// (no copy). Do not modify b while using the returned Frame.
func DecodeFrame(b []byte) (Frame, error) {
	if len(b) < offPayload {
		return Frame{}, fmt.Errorf("binary.DecodeFrame: frame too short (%d bytes)", len(b))
	}
	return Frame{
		TypeTag:      b[offType],
		InstrumentID: getI32(b, offInstrument),
		Resolution:   b[offResolution],
		Timestamp:    getI64(b, offTimestamp),
		Payload:      b[offPayload:],
	}, nil
}

// ============================================================
// Frame convenience accessors
// ============================================================

// ScalarValue extracts the float64 from a scalar frame payload.
func (f Frame) ScalarValue() (float64, error) { return DecodeScalar(f.Payload) }

// OHLCValues extracts open/high/low/close from an OHLC frame payload.
func (f Frame) OHLCValues() (open, high, low, close float64, err error) { return DecodeOHLC(f.Payload) }

// BBValues extracts upper/middle/lower from a BB frame payload.
func (f Frame) BBValues() (upper, middle, lower float64, err error) { return DecodeBB(f.Payload) }

// MACDValues extracts macdLine/signalLine/histogram from a MACD frame payload.
func (f Frame) MACDValues() (macdLine, signalLine, histogram float64, err error) {
	return DecodeMACD(f.Payload)
}
