package raknet

import (
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	triadHalfRange         = uint32(1 << 23)
	maxIncomingDatagramGap = uint32(4096)
	reliableReceiveWindow  = uint32(4096)
	maxOrderedPacketGap    = uint32(4096)
	maxPendingOrderPacket  = 4096
	maxSplitFragmentCount  = uint32(4096)
	maxSplitPayloadBytes   = 8 * 1024 * 1024
	maxPendingSplit        = 64
	maxPendingSplitBytes   = 16 * 1024 * 1024
	splitAssemblyLifetime  = 30 * time.Second
)

type receiveSequenceWindow struct {
	isInitialized bool
	latest        uint32
	seenSequences map[uint32]struct{}
}

type incomingSplitAssembly struct {
	reliability   Reliability
	orderIndex    uint32
	orderChannel  uint8
	fragments     [][]byte
	receivedCount uint32
	byteCount     int
	updatedAt     time.Time
}

type receiveState struct {
	isDatagramInitialized bool
	nextDatagram          uint32
	futureDatagrams       map[uint32]struct{}
	missingDatagrams      map[uint32]struct{}
	reliable              receiveSequenceWindow
	nextOrderIndex        [32]uint32
	pendingOrders         [32]map[uint32]EncapsulatedPacket
	pendingOrderCount     int
	isSequenceInitialized [32]bool
	latestSequenceIndex   [32]uint32
	splitAssemblies       map[uint16]*incomingSplitAssembly
	splitByteCount        int
}

func triadForwardDistance(from uint32, to uint32) uint32 {
	return (to - from) & 0x00ffffff
}

func (s *Server) acceptIncomingDatagram(
	address *net.UDPAddr, sequence uint32,
) (bool, []uint32, error) {
	if address == nil {
		return false, nil, errors.New("address missing")
	}
	sequence &= 0x00ffffff
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		return false, nil, errors.New("peer unknown")
	}
	currentPeer.lastReceive = time.Now()
	state := &currentPeer.receive
	if !state.isDatagramInitialized {
		state.isDatagramInitialized = true
		state.nextDatagram = advanceTriad(sequence, 1)
		return true, nil, nil
	}
	if sequence == state.nextDatagram {
		state.acceptExpectedDatagram()
		return true, nil, nil
	}
	distance := triadForwardDistance(state.nextDatagram, sequence)
	if distance >= triadHalfRange {
		return false, nil, nil
	}
	if distance > maxIncomingDatagramGap {
		state.slideDatagramWindow(sequence)
		if sequence == state.nextDatagram {
			state.acceptExpectedDatagram()
			return true, nil, nil
		}
		distance = triadForwardDistance(state.nextDatagram, sequence)
	}
	if state.futureDatagrams == nil {
		state.futureDatagrams = make(map[uint32]struct{})
	}
	if _, isFound := state.futureDatagrams[sequence]; isFound {
		return false, nil, nil
	}
	state.futureDatagrams[sequence] = struct{}{}
	delete(state.missingDatagrams, sequence)
	if state.missingDatagrams == nil {
		state.missingDatagrams = make(map[uint32]struct{})
	}
	missingSequences := make([]uint32, 0, distance)
	for missing := state.nextDatagram; missing != sequence; missing = advanceTriad(missing, 1) {
		if _, isReceived := state.futureDatagrams[missing]; isReceived {
			continue
		}
		if _, isKnown := state.missingDatagrams[missing]; isKnown {
			continue
		}
		state.missingDatagrams[missing] = struct{}{}
		missingSequences = append(missingSequences, missing)
	}
	return true, missingSequences, nil
}

func (state *receiveState) acceptExpectedDatagram() {
	delete(state.missingDatagrams, state.nextDatagram)
	state.nextDatagram = advanceTriad(state.nextDatagram, 1)
	state.advanceReceivedDatagrams()
}

func (state *receiveState) advanceReceivedDatagrams() {
	for {
		if _, isFound := state.futureDatagrams[state.nextDatagram]; !isFound {
			return
		}
		delete(state.futureDatagrams, state.nextDatagram)
		delete(state.missingDatagrams, state.nextDatagram)
		state.nextDatagram = advanceTriad(state.nextDatagram, 1)
	}
}

func (state *receiveState) slideDatagramWindow(sequence uint32) {
	state.nextDatagram = (sequence - maxIncomingDatagramGap) & 0x00ffffff
	for received := range state.futureDatagrams {
		distance := triadForwardDistance(state.nextDatagram, received)
		if distance >= maxIncomingDatagramGap {
			delete(state.futureDatagrams, received)
		}
	}
	for missing := range state.missingDatagrams {
		distance := triadForwardDistance(state.nextDatagram, missing)
		if distance >= maxIncomingDatagramGap {
			delete(state.missingDatagrams, missing)
		}
	}
	state.advanceReceivedDatagrams()
}

func (s *Server) prepareIncomingPackets(
	address *net.UDPAddr, packets []EncapsulatedPacket,
) ([]EncapsulatedPacket, error) {
	if address == nil {
		return nil, errors.New("address missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		return nil, errors.New("peer unknown")
	}
	state := &currentPeer.receive
	now := time.Now()
	state.expireSplits(now)
	readyPackets := make([]EncapsulatedPacket, 0, len(packets))
	for index, packet := range packets {
		if packet.Reliability > ReliableSequenced {
			return readyPackets, fmt.Errorf(
				"packet[%d] wire reliability %d unsupported", index, packet.Reliability,
			)
		}
		if reliabilityHasMessageIndex(packet.Reliability) &&
			!state.reliable.accept(packet.MessageIndex) {
			continue
		}
		if packet.IsSplit {
			reassembled, err := state.acceptSplit(packet, now)
			if err != nil {
				return readyPackets, fmt.Errorf("packet[%d] split: %w", index, err)
			}
			if reassembled == nil {
				continue
			}
			packet = *reassembled
		}
		switch packet.Reliability {
		case ReliableOrdered:
			orderedPackets, err := state.acceptOrdered(packet)
			if err != nil {
				return readyPackets, fmt.Errorf("packet[%d] ordered: %w", index, err)
			}
			readyPackets = append(readyPackets, orderedPackets...)
		case UnreliableSequenced, ReliableSequenced:
			isAccepted, err := state.acceptSequenced(packet)
			if err != nil {
				return readyPackets, fmt.Errorf("packet[%d] sequenced: %w", index, err)
			}
			if isAccepted {
				readyPackets = append(readyPackets, packet)
			}
		default:
			readyPackets = append(readyPackets, packet)
		}
	}
	return readyPackets, nil
}

func (window *receiveSequenceWindow) accept(sequence uint32) bool {
	sequence &= 0x00ffffff
	if window.seenSequences == nil {
		window.seenSequences = make(map[uint32]struct{})
	}
	if !window.isInitialized {
		window.isInitialized = true
		window.latest = sequence
		window.seenSequences[sequence] = struct{}{}
		return true
	}
	if _, isFound := window.seenSequences[sequence]; isFound {
		return false
	}
	forward := triadForwardDistance(window.latest, sequence)
	if forward != 0 && forward < triadHalfRange {
		window.latest = sequence
		for received := range window.seenSequences {
			if triadForwardDistance(received, window.latest) >= reliableReceiveWindow {
				delete(window.seenSequences, received)
			}
		}
		window.seenSequences[sequence] = struct{}{}
		return true
	}
	if triadForwardDistance(sequence, window.latest) >= reliableReceiveWindow {
		return false
	}
	window.seenSequences[sequence] = struct{}{}
	return true
}

func (state *receiveState) acceptOrdered(
	packet EncapsulatedPacket,
) ([]EncapsulatedPacket, error) {
	channel := packet.OrderChannel
	if channel >= uint8(len(state.nextOrderIndex)) {
		return nil, fmt.Errorf("channel %d out of range", channel)
	}
	expected := state.nextOrderIndex[channel]
	if packet.OrderIndex == expected {
		readyPackets := []EncapsulatedPacket{packet}
		state.nextOrderIndex[channel] = advanceTriad(expected, 1)
		for {
			next := state.nextOrderIndex[channel]
			buffered, isFound := state.pendingOrders[channel][next]
			if !isFound {
				break
			}
			delete(state.pendingOrders[channel], next)
			state.pendingOrderCount--
			readyPackets = append(readyPackets, buffered)
			state.nextOrderIndex[channel] = advanceTriad(next, 1)
		}
		return readyPackets, nil
	}
	distance := triadForwardDistance(expected, packet.OrderIndex)
	if distance >= triadHalfRange {
		return nil, nil
	}
	if distance > maxOrderedPacketGap {
		return nil, fmt.Errorf(
			"index gap %d exceeds %d", distance, maxOrderedPacketGap,
		)
	}
	if state.pendingOrders[channel] == nil {
		state.pendingOrders[channel] = make(map[uint32]EncapsulatedPacket)
	}
	if _, isFound := state.pendingOrders[channel][packet.OrderIndex]; isFound {
		return nil, nil
	}
	if state.pendingOrderCount >= maxPendingOrderPacket {
		return nil, errors.New("pending window full")
	}
	state.pendingOrders[channel][packet.OrderIndex] = cloneEncapsulatedPacket(packet)
	state.pendingOrderCount++
	return nil, nil
}

func (state *receiveState) acceptSequenced(
	packet EncapsulatedPacket,
) (bool, error) {
	channel := packet.OrderChannel
	if channel >= uint8(len(state.latestSequenceIndex)) {
		return false, fmt.Errorf("channel %d out of range", channel)
	}
	if !state.isSequenceInitialized[channel] {
		state.isSequenceInitialized[channel] = true
		state.latestSequenceIndex[channel] = packet.OrderIndex
		return true, nil
	}
	distance := triadForwardDistance(
		state.latestSequenceIndex[channel], packet.OrderIndex,
	)
	if distance == 0 || distance >= triadHalfRange {
		return false, nil
	}
	state.latestSequenceIndex[channel] = packet.OrderIndex
	return true, nil
}

func (state *receiveState) acceptSplit(
	packet EncapsulatedPacket, now time.Time,
) (*EncapsulatedPacket, error) {
	if packet.SplitCount == 0 || packet.SplitCount > maxSplitFragmentCount {
		return nil, fmt.Errorf("count %d out of range", packet.SplitCount)
	}
	if packet.SplitIndex >= packet.SplitCount {
		return nil, fmt.Errorf(
			"index %d exceeds count %d", packet.SplitIndex, packet.SplitCount,
		)
	}
	if state.splitAssemblies == nil {
		state.splitAssemblies = make(map[uint16]*incomingSplitAssembly)
	}
	assembly := state.splitAssemblies[packet.SplitID]
	if assembly == nil {
		if len(state.splitAssemblies) >= maxPendingSplit {
			return nil, errors.New("assembly window full")
		}
		assembly = &incomingSplitAssembly{
			reliability:  packet.Reliability,
			orderIndex:   packet.OrderIndex,
			orderChannel: packet.OrderChannel,
			fragments:    make([][]byte, packet.SplitCount),
			updatedAt:    now,
		}
		state.splitAssemblies[packet.SplitID] = assembly
	}
	if uint32(len(assembly.fragments)) != packet.SplitCount ||
		assembly.reliability != packet.Reliability ||
		assembly.orderIndex != packet.OrderIndex ||
		assembly.orderChannel != packet.OrderChannel {
		return nil, errors.New("metadata changed")
	}
	assembly.updatedAt = now
	if assembly.fragments[packet.SplitIndex] != nil {
		return nil, nil
	}
	if assembly.byteCount+len(packet.Payload) > maxSplitPayloadBytes ||
		state.splitByteCount+len(packet.Payload) > maxPendingSplitBytes {
		state.removeSplit(packet.SplitID)
		return nil, errors.New("payload window full")
	}
	assembly.fragments[packet.SplitIndex] = append([]byte(nil), packet.Payload...)
	assembly.receivedCount++
	assembly.byteCount += len(packet.Payload)
	state.splitByteCount += len(packet.Payload)
	if assembly.receivedCount != packet.SplitCount {
		return nil, nil
	}
	payload := make([]byte, 0, assembly.byteCount)
	for _, fragment := range assembly.fragments {
		payload = append(payload, fragment...)
	}
	reassembled := cloneEncapsulatedPacket(packet)
	reassembled.IsSplit = false
	reassembled.SplitCount = 0
	reassembled.SplitID = 0
	reassembled.SplitIndex = 0
	reassembled.Payload = payload
	state.removeSplit(packet.SplitID)
	return &reassembled, nil
}

func (state *receiveState) expireSplits(now time.Time) {
	for splitID, assembly := range state.splitAssemblies {
		if now.Sub(assembly.updatedAt) >= splitAssemblyLifetime {
			state.removeSplit(splitID)
		}
	}
}

func (state *receiveState) removeSplit(splitID uint16) {
	assembly := state.splitAssemblies[splitID]
	if assembly == nil {
		return
	}
	state.splitByteCount -= assembly.byteCount
	delete(state.splitAssemblies, splitID)
}

func cloneEncapsulatedPacket(packet EncapsulatedPacket) EncapsulatedPacket {
	packet.Payload = append([]byte(nil), packet.Payload...)
	return packet
}
