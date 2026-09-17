package raknet

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type Reliability uint8

const (
	Unreliable Reliability = iota
	UnreliableSequenced
	Reliable
	ReliableOrdered
	ReliableSequenced
	UnreliableWithACKReceipt
	ReliableWithACKReceipt
	ReliableOrderedWithACKReceipt
)

type EncapsulatedPacket struct {
	Reliability              Reliability
	MessageIndex, OrderIndex uint32
	OrderChannel             uint8
	IsSplit                  bool
	SplitCount, SplitIndex   uint32
	SplitID                  uint16
	Payload                  []byte
}

type Datagram struct {
	Flags    uint8
	Sequence uint32
	Packets  []EncapsulatedPacket
}

const maxEncapsulatedPacketCount = 4096

func DecodeDatagram(data []byte) (Datagram, error) {
	if len(data) < 4 || data[0]&0x80 == 0 {
		return Datagram{}, errors.New("raknet: invalid data datagram")
	}
	datagram := Datagram{Flags: data[0], Sequence: readTriad(data[1:4])}
	offset := 4
	for offset < len(data) {
		if len(datagram.Packets) >= maxEncapsulatedPacketCount {
			return Datagram{}, errors.New("raknet: encapsulated packet limit exceeded")
		}
		packet, consumed, err := decodeEncapsulated(data[offset:])
		if err != nil {
			return Datagram{}, fmt.Errorf("packetDecode[%d]: %w", offset, err)
		}
		offset += consumed
		datagram.Packets = append(datagram.Packets, packet)
	}
	return datagram, nil
}

func EncodeDatagram(datagram Datagram) ([]byte, error) {
	writer := NewWriter()
	flags := datagram.Flags
	if flags&0x80 == 0 {
		flags = 0x84
	}
	writer.Uint8(flags)
	writeTriad(writer, datagram.Sequence)
	for index, packet := range datagram.Packets {
		contents, err := encodeEncapsulated(packet)
		if err != nil {
			return nil, fmt.Errorf("packetEncode[%d]: %w", index, err)
		}
		writer.WriteBytes(contents)
	}
	return writer.Bytes(), nil
}

func decodeEncapsulated(data []byte) (EncapsulatedPacket, int, error) {
	if len(data) < 3 {
		return EncapsulatedPacket{}, 0, ErrShortBuffer
	}
	flags := data[0]
	packet := EncapsulatedPacket{Reliability: Reliability(flags >> 5), IsSplit: flags&0x10 != 0}
	bitLength := int(binary.BigEndian.Uint16(data[1:3]))
	offset := 3
	readIndex := func() (uint32, error) {
		if len(data)-offset < 3 {
			return 0, ErrShortBuffer
		}
		value := readTriad(data[offset : offset+3])
		offset += 3
		return value, nil
	}
	var err error
	if reliabilityHasMessageIndex(packet.Reliability) {
		packet.MessageIndex, err = readIndex()
		if err != nil {
			return EncapsulatedPacket{}, 0, fmt.Errorf("messageIndex: %w", err)
		}
	}
	if reliabilitySequenced(packet.Reliability) || reliabilityOrdered(packet.Reliability) {
		packet.OrderIndex, err = readIndex()
		if err != nil {
			return EncapsulatedPacket{}, 0, fmt.Errorf("orderIndex: %w", err)
		}
		if len(data)-offset < 1 {
			return EncapsulatedPacket{}, 0, ErrShortBuffer
		}
		packet.OrderChannel = data[offset]
		offset++
	}
	if packet.IsSplit {
		if len(data)-offset < 10 {
			return EncapsulatedPacket{}, 0, ErrShortBuffer
		}
		packet.SplitCount = binary.BigEndian.Uint32(data[offset : offset+4])
		packet.SplitID = binary.BigEndian.Uint16(data[offset+4 : offset+6])
		packet.SplitIndex = binary.BigEndian.Uint32(data[offset+6 : offset+10])
		offset += 10
	}
	byteLength := (bitLength + 7) / 8
	if len(data)-offset < byteLength {
		return EncapsulatedPacket{}, 0, ErrShortBuffer
	}
	packet.Payload = append([]byte(nil), data[offset:offset+byteLength]...)
	return packet, offset + byteLength, nil
}

func encodeEncapsulated(packet EncapsulatedPacket) ([]byte, error) {
	if len(packet.Payload) > 8191 {
		return nil, errors.New("raknet: encapsulated payload too large")
	}
	writer := NewWriter()
	flags := uint8(packet.Reliability << 5)
	if packet.IsSplit {
		flags |= 0x10
	}
	writer.Uint8(flags)
	writer.Uint16(uint16(len(packet.Payload) * 8))
	if reliabilityHasMessageIndex(packet.Reliability) {
		writeTriad(writer, packet.MessageIndex)
	}
	if reliabilitySequenced(packet.Reliability) || reliabilityOrdered(packet.Reliability) {
		writeTriad(writer, packet.OrderIndex)
		writer.Uint8(packet.OrderChannel)
	}
	if packet.IsSplit {
		writer.Uint32(packet.SplitCount)
		writer.Uint16(packet.SplitID)
		writer.Uint32(packet.SplitIndex)
	}
	writer.WriteBytes(packet.Payload)
	return writer.Bytes(), nil
}

func EncodeACK(sequences ...uint32) []byte {
	return encodeAcknowledgement(0xc0, sequences)
}

func EncodeNACK(sequences ...uint32) []byte {
	return encodeAcknowledgement(0xa0, sequences)
}

func encodeAcknowledgement(packetID byte, sequences []uint32) []byte {
	type sequenceRange struct {
		start uint32
		end   uint32
	}
	ranges := make([]sequenceRange, 0, len(sequences))
	for _, sequence := range sequences {
		sequence &= 0x00ffffff
		if len(ranges) == 0 {
			ranges = append(ranges, sequenceRange{start: sequence, end: sequence})
			continue
		}
		last := &ranges[len(ranges)-1]
		if last.end != 0x00ffffff && sequence == last.end+1 {
			last.end = sequence
			continue
		}
		if sequence == last.end {
			continue
		}
		ranges = append(ranges, sequenceRange{start: sequence, end: sequence})
	}
	writer := NewWriter()
	writer.Uint8(packetID)
	writer.Uint16(uint16(len(ranges)))
	for _, sequenceRange := range ranges {
		isSingle := sequenceRange.start == sequenceRange.end
		writer.Bool8(isSingle)
		writeTriad(writer, sequenceRange.start)
		if !isSingle {
			writeTriad(writer, sequenceRange.end)
		}
	}
	return writer.Bytes()
}

func DecodeACK(data []byte) ([]uint32, error) {
	if len(data) < 3 || data[0] != 0xc0 && data[0] != 0xa0 {
		return nil, errors.New("raknet: invalid ACK/NACK")
	}
	count := int(binary.BigEndian.Uint16(data[1:3]))
	if count > maxPendingDatagram {
		return nil, errors.New("raknet: ACK/NACK record limit exceeded")
	}
	offset := 3
	values := make([]uint32, 0, count)
	for index := 0; index < count; index++ {
		if len(data)-offset < 4 {
			return nil, ErrShortBuffer
		}
		isSingle := data[offset] != 0
		offset++
		start := readTriad(data[offset : offset+3])
		offset += 3
		end := start
		if !isSingle {
			if len(data)-offset < 3 {
				return nil, ErrShortBuffer
			}
			end = readTriad(data[offset : offset+3])
			offset += 3
		}
		if end < start || end-start > 65535 {
			return nil, errors.New("raknet: invalid ACK range")
		}
		rangeCount := int(end-start) + 1
		if rangeCount > maxPendingDatagram-len(values) {
			return nil, errors.New("raknet: ACK/NACK sequence limit exceeded")
		}
		for value := start; value <= end; value++ {
			values = append(values, value)
		}
	}
	return values, nil
}

func readTriad(data []byte) uint32 { return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 }
func writeTriad(writer *Writer, value uint32) {
	writer.Uint8(uint8(value))
	writer.Uint8(uint8(value >> 8))
	writer.Uint8(uint8(value >> 16))
}
func reliabilityHasMessageIndex(value Reliability) bool {
	return value == Reliable || value == ReliableOrdered || value == ReliableSequenced || value == ReliableWithACKReceipt || value == ReliableOrderedWithACKReceipt
}
func reliabilitySequenced(value Reliability) bool {
	return value == UnreliableSequenced || value == ReliableSequenced
}
func reliabilityOrdered(value Reliability) bool {
	return value == UnreliableSequenced || value == ReliableOrdered || value == ReliableSequenced || value == ReliableOrderedWithACKReceipt
}
