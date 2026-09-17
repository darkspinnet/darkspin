// Package blaze implements the Blaze RPC transport and components.
package blaze

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/darkspinnet/darkspin/server/databuffer"
)

const frameHeaderSize = 12

// MessageType identifies the Blaze RPC message class.
type MessageType uint8

const (
	Message MessageType = iota
	Reply
	Notification
	ErrorReply
)

// Frame is one Blaze RPC header and its TDF payload.
type Frame struct {
	Component uint16
	Command   uint16
	ErrorCode uint16
	Type      MessageType
	MessageID uint32
	Payload   []byte
}

// ReadFrame reads exactly one frame from a stream.
func ReadFrame(reader io.Reader) (Frame, error) {
	header := make([]byte, frameHeaderSize)
	_, err := io.ReadFull(reader, header)
	if err != nil {
		return Frame{}, fmt.Errorf("headerRead: %w", err)
	}
	length := int(header[0])<<8 | int(header[1])
	payload := make([]byte, length)
	_, err = io.ReadFull(reader, payload)
	if err != nil {
		return Frame{}, fmt.Errorf("payloadRead: %w", err)
	}
	packet := append(header, payload...)
	frame, err := DecodeFrame(databuffer.FromBytes(packet))
	if err != nil {
		return Frame{}, fmt.Errorf("streamDecode: %w", err)
	}
	return frame, nil
}

// WriteFrame writes exactly one frame to a stream.
func WriteFrame(writer io.Writer, frame Frame) error {
	encoded, err := EncodeFrame(frame)
	if err != nil {
		return fmt.Errorf("frameEncode: %w", err)
	}
	for len(encoded) > 0 {
		count, writeErr := writer.Write(encoded)
		if writeErr != nil {
			return fmt.Errorf("frameWrite: %w", writeErr)
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		encoded = encoded[count:]
	}
	return nil
}

// EncodeFrame serializes a Blaze frame using the 12-byte big-endian header used
// by recap_server.
func EncodeFrame(frame Frame) ([]byte, error) {
	if len(frame.Payload) > math.MaxUint16 {
		return nil, errors.New("encode Blaze frame: payload exceeds 65535 bytes")
	}
	if frame.Type > 0x0f {
		return nil, fmt.Errorf("encode Blaze frame: invalid message type %d", frame.Type)
	}
	if frame.MessageID > 0x000fffff {
		return nil, errors.New("encode Blaze frame: message ID exceeds 20 bits")
	}

	buffer := databuffer.New()
	buffer.WriteUint16BE(uint16(len(frame.Payload)))
	buffer.WriteUint16BE(frame.Component)
	buffer.WriteUint16BE(frame.Command)
	buffer.WriteUint16BE(frame.ErrorCode)
	message := uint32(frame.Type)<<28 | frame.MessageID
	buffer.WriteUint32BE(message)
	buffer.WriteBytes(frame.Payload)
	return buffer.Bytes(), nil
}

// DecodeFrame reads one Blaze frame and leaves any following frame untouched.
func DecodeFrame(buffer *databuffer.Buffer) (Frame, error) {
	if buffer == nil {
		return Frame{}, errors.New("decode Blaze frame: nil buffer")
	}
	if buffer.Size()-buffer.Position() < frameHeaderSize {
		return Frame{}, errors.New("decode Blaze frame: incomplete header")
	}

	length, err := buffer.ReadUint16BE()
	if err != nil {
		return Frame{}, fmt.Errorf("lengthDecode: %w", err)
	}
	component, err := buffer.ReadUint16BE()
	if err != nil {
		return Frame{}, fmt.Errorf("componentDecode: %w", err)
	}
	command, err := buffer.ReadUint16BE()
	if err != nil {
		return Frame{}, fmt.Errorf("commandDecode: %w", err)
	}
	errorCode, err := buffer.ReadUint16BE()
	if err != nil {
		return Frame{}, fmt.Errorf("errorDecode: %w", err)
	}
	message, err := buffer.ReadUint32BE()
	if err != nil {
		return Frame{}, fmt.Errorf("messageDecode: %w", err)
	}
	payload, err := buffer.ReadBytes(int(length))
	if err != nil {
		return Frame{}, fmt.Errorf("payloadDecode: %w", err)
	}

	return Frame{
		Component: component,
		Command:   command,
		ErrorCode: errorCode,
		Type:      MessageType(message >> 28),
		MessageID: message & 0x000fffff,
		Payload:   payload,
	}, nil
}
