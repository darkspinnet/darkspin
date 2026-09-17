package raknet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	maxPendingDatagram         = 4096
	maxRetransmissionAttempt   = 8
	retransmissionInitialDelay = 500 * time.Millisecond
	retransmissionMaximumDelay = 2 * time.Second
	retransmissionSweepDelay   = 100 * time.Millisecond
	peerIdleTimeout            = 2 * time.Minute
)

type outboundDatagram struct {
	sequence     uint32
	payload      []byte
	lastSent     time.Time
	attemptCount uint8
}

type retransmissionPublication struct {
	address  *net.UDPAddr
	peer     *peer
	sequence uint32
	payload  []byte
}

type peerExpiryCandidate struct {
	address *net.UDPAddr
	peer    *peer
}

func newOutboundDatagram(sequence uint32, payload []byte) outboundDatagram {
	return outboundDatagram{
		sequence: sequence, payload: append([]byte(nil), payload...),
		lastSent: time.Now(), attemptCount: 1,
	}
}

func advanceTriad(index uint32, count uint32) uint32 {
	return (index + count) & 0x00ffffff
}

func (s *Server) retainDatagrams(
	address *net.UDPAddr, expectedPeer *peer, datagrams []outboundDatagram,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil || currentPeer != expectedPeer {
		return errors.New("retransmission peer changed")
	}
	if currentPeer.pendingDatagrams == nil {
		currentPeer.pendingDatagrams = make(map[uint32]outboundDatagram)
	}
	if len(currentPeer.pendingDatagrams)+len(datagrams) > maxPendingDatagram {
		return errors.New("retransmission window full")
	}
	for _, datagram := range datagrams {
		if _, isFound := currentPeer.pendingDatagrams[datagram.sequence]; isFound {
			return fmt.Errorf("retransmission sequence %d retained twice", datagram.sequence)
		}
		currentPeer.pendingDatagrams[datagram.sequence] = datagram
	}
	return nil
}

func (s *Server) handleAcknowledgement(
	address *net.UDPAddr, isNACK bool, sequences []uint32,
) ([][]byte, error) {
	if address == nil {
		return nil, errors.New("acknowledgement address missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		return nil, nil
	}
	currentPeer.lastReceive = time.Now()
	if !isNACK {
		for _, sequence := range sequences {
			delete(currentPeer.pendingDatagrams, sequence)
		}
		return nil, nil
	}
	now := time.Now()
	responses := make([][]byte, 0, len(sequences))
	handledSequence := make(map[uint32]struct{}, len(sequences))
	for _, sequence := range sequences {
		if _, isHandled := handledSequence[sequence]; isHandled {
			continue
		}
		handledSequence[sequence] = struct{}{}
		datagram, isFound := currentPeer.pendingDatagrams[sequence]
		if !isFound || datagram.attemptCount >= maxRetransmissionAttempt {
			continue
		}
		datagram.lastSent = now
		datagram.attemptCount++
		currentPeer.pendingDatagrams[sequence] = datagram
		responses = append(responses, append([]byte(nil), datagram.payload...))
	}
	return responses, nil
}

func retransmissionDelay(attemptCount uint8) time.Duration {
	delay := retransmissionInitialDelay
	for attempt := uint8(1); attempt < attemptCount; attempt++ {
		delay *= 2
		if delay >= retransmissionMaximumDelay {
			return retransmissionMaximumDelay
		}
	}
	return delay
}

func (s *Server) collectDueRetransmissions(
	now time.Time, expectedConn *net.UDPConn, expectedGeneration uint64,
) ([]retransmissionPublication, []peerExpiryCandidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != expectedConn || s.connGeneration != expectedGeneration {
		return nil, nil
	}
	publications := make([]retransmissionPublication, 0)
	expired := make([]peerExpiryCandidate, 0)
	for _, currentPeer := range s.peers {
		if currentPeer.address == nil {
			continue
		}
		if !currentPeer.lastReceive.IsZero() &&
			now.Sub(currentPeer.lastReceive) >= peerIdleTimeout {
			expired = append(expired, peerExpiryCandidate{
				address: cloneUDPAddress(currentPeer.address), peer: currentPeer,
			})
			continue
		}
		isExpired := false
		peerPublications := make([]retransmissionPublication, 0)
		for sequence, datagram := range currentPeer.pendingDatagrams {
			if now.Sub(datagram.lastSent) < retransmissionDelay(datagram.attemptCount) {
				continue
			}
			if datagram.attemptCount >= maxRetransmissionAttempt {
				isExpired = true
				break
			}
			address := *currentPeer.address
			address.IP = append(net.IP(nil), currentPeer.address.IP...)
			peerPublications = append(peerPublications, retransmissionPublication{
				address: &address,
				peer:    currentPeer, sequence: sequence,
				payload: append([]byte(nil), datagram.payload...),
			})
		}
		if !isExpired {
			publications = append(publications, peerPublications...)
			continue
		}
		expired = append(expired, peerExpiryCandidate{
			address: cloneUDPAddress(currentPeer.address), peer: currentPeer,
		})
	}
	return publications, expired
}

func (s *Server) removeExpiredPeer(
	candidate peerExpiryCandidate, now time.Time,
) bool {
	if candidate.address == nil || candidate.peer == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[candidate.address.String()]
	if currentPeer != candidate.peer {
		return false
	}
	isExpired := !currentPeer.lastReceive.IsZero() &&
		now.Sub(currentPeer.lastReceive) >= peerIdleTimeout
	if !isExpired {
		for _, datagram := range currentPeer.pendingDatagrams {
			if datagram.attemptCount >= maxRetransmissionAttempt {
				isExpired = true
				break
			}
		}
	}
	if !isExpired {
		return false
	}
	delete(s.peers, candidate.address.String())
	return true
}

func (s *Server) runRetransmission(
	ctx context.Context,
	conn *net.UDPConn, generation uint64,
) {
	ticker := time.NewTicker(retransmissionSweepDelay)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			publications, expiryCandidate := s.collectDueRetransmissions(
				now, conn, generation,
			)
			expired := make([]peerDeparture, 0, len(expiryCandidate))
			for _, publication := range publications {
				if !publication.peer.outboundMu.TryLock() {
					continue
				}
				s.mu.Lock()
				isCurrent := s.peers[publication.address.String()] == publication.peer &&
					s.conn == conn && s.connGeneration == generation
				datagram, isPending := publication.peer.pendingDatagrams[publication.sequence]
				isDue := isPending &&
					time.Since(datagram.lastSent) >= retransmissionDelay(datagram.attemptCount) &&
					datagram.attemptCount < maxRetransmissionAttempt
				s.mu.Unlock()
				if !isCurrent || !isDue {
					publication.peer.outboundMu.Unlock()
					continue
				}
				s.outboundMu.RLock()
				s.mu.Lock()
				isCurrent = s.peers[publication.address.String()] == publication.peer &&
					s.conn == conn && s.connGeneration == generation
				datagram, isPending = publication.peer.pendingDatagrams[publication.sequence]
				isDue = isPending &&
					time.Since(datagram.lastSent) >= retransmissionDelay(datagram.attemptCount) &&
					datagram.attemptCount < maxRetransmissionAttempt
				if isCurrent && isDue {
					datagram.lastSent = time.Now()
					datagram.attemptCount++
					publication.peer.pendingDatagrams[publication.sequence] = datagram
					publication.payload = append([]byte(nil), datagram.payload...)
				}
				s.mu.Unlock()
				if !isCurrent || !isDue {
					s.outboundMu.RUnlock()
					publication.peer.outboundMu.Unlock()
					continue
				}
				_, err := conn.WriteToUDP(publication.payload, publication.address)
				if err == nil {
					s.observeDatagram(
						ctx, ObservationServerToClient, ObservationRetransmit,
						publication.address, publication.payload,
					)
				}
				s.outboundMu.RUnlock()
				publication.peer.outboundMu.Unlock()
				if err != nil {
					isRemoved := s.removeExpectedPeer(
						publication.address, publication.peer,
					)
					if isRemoved {
						expired = append(expired, peerDeparture{
							address:    publication.address,
							generation: publication.peer.generation,
						})
					}
				}
			}
			for _, candidate := range expiryCandidate {
				if !candidate.peer.outboundMu.TryLock() {
					continue
				}
				isRemoved := s.removeExpiredPeer(candidate, time.Now())
				candidate.peer.outboundMu.Unlock()
				if isRemoved {
					expired = append(expired, peerDeparture{
						address:    candidate.address,
						generation: candidate.peer.generation,
					})
				}
			}
			for _, candidate := range s.connectedPollCandidates(conn, generation) {
				err := s.pollAndWriteConnected(ctx, conn, generation, candidate)
				if err != nil {
					s.reportScheduleError(fmt.Errorf("connectedPoll: %w", err))
				}
			}
			if len(expired) != 0 {
				departures := append([]peerDeparture(nil), expired...)
				go s.notifyExpiredPeers(departures)
			}
		}
	}
}

func (s *Server) connectedPollCandidates(
	conn *net.UDPConn, generation uint64,
) []peerExpiryCandidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != conn || s.connGeneration != generation || s.pollHandler == nil {
		return nil
	}
	candidates := make([]peerExpiryCandidate, 0, len(s.peers))
	for _, currentPeer := range s.peers {
		if !currentPeer.isConnected || currentPeer.address == nil {
			continue
		}
		candidates = append(candidates, peerExpiryCandidate{
			address: cloneUDPAddress(currentPeer.address), peer: currentPeer,
		})
	}
	return candidates
}

func (s *Server) pollAndWriteConnected(
	ctx context.Context, conn *net.UDPConn, generation uint64,
	candidate peerExpiryCandidate,
) error {
	if candidate.address == nil || candidate.peer == nil ||
		!candidate.peer.outboundMu.TryLock() {
		return nil
	}
	defer candidate.peer.outboundMu.Unlock()
	s.outboundMu.RLock()
	defer s.outboundMu.RUnlock()
	s.mu.Lock()
	isCurrent := s.conn == conn && s.connGeneration == generation &&
		s.peers[candidate.address.String()] == candidate.peer &&
		candidate.peer.isConnected
	s.mu.Unlock()
	if !isCurrent {
		return nil
	}
	commitCollector := &responseCommitCollector{}
	pollContext := context.WithValue(
		ctx, responseCommitContextKey{}, commitCollector,
	)
	responses, err := s.pollConnected(pollContext, candidate.address)
	if err != nil {
		return fmt.Errorf("pollHandle: %w", err)
	}
	for index, response := range responses {
		_, err = conn.WriteToUDP(response, candidate.address)
		if err != nil {
			return fmt.Errorf("pollWrite[%d]: %w", index, err)
		}
	}
	err = commitCollector.commit()
	if err != nil {
		return fmt.Errorf("pollCommit: %w", err)
	}
	return nil
}

func (s *Server) notifyExpiredPeers(departures []peerDeparture) {
	if len(departures) == 0 {
		return
	}
	s.mu.Lock()
	handler := s.peerReplaced
	s.mu.Unlock()
	if handler == nil {
		return
	}
	for index, departure := range departures {
		err := invokeCommit(func() {
			handler(departure.address, departure.generation)
		})
		if err != nil {
			s.reportScheduleError(fmt.Errorf(
				"peerExpiryCallback[%d]: %w", index, err,
			))
		}
	}
}
