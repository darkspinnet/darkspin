package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// Filter only client input acknowledgements. Effects, cooldowns, resources,
// damage and deferred hit commits retain their ordinary transport behavior.
func playerAIPresentation(packets [][]byte) [][]byte {
	presentations := make([][]byte, 0, len(packets))
	for _, packet := range packets {
		if len(packet) != 0 && packet[0] != byte(raknet.ActionCommandResponse) {
			presentations = append(presentations, packet)
		}
	}
	return presentations
}

type playerAISchedule struct{ packet raknet.Packet }
type playerAIProducer struct{ produce func() ([][]byte, error) }

func (e playerAIProducer) run() ([][]byte, error) {
	packets, err := e.produce()
	if err != nil {
		return nil, fmt.Errorf("aiProducer: %w", err)
	}
	return playerAIPresentation(packets), nil
}

func playerAIPacket(packet raknet.Packet) raknet.Packet {
	schedule := playerAISchedule{packet: packet}
	if packet.Schedule != nil {
		packet.Schedule = schedule.fixed
	}
	if packet.ScheduleFunc != nil {
		packet.ScheduleFunc = schedule.dynamic
	}
	if packet.ScheduleGroup != nil {
		packet.ScheduleGroup = schedule.group
	}
	if packet.ScheduleGroupResult != nil {
		packet.ScheduleGroupResult = schedule.groupResult
	}
	return packet
}

func (e playerAISchedule) fixed(delay time.Duration, packets [][]byte) error {
	err := e.packet.Schedule(delay, playerAIPresentation(packets))
	if err != nil {
		return fmt.Errorf("aiSchedule: %w", err)
	}
	return nil
}

func (e playerAISchedule) dynamic(delay time.Duration, produce func() ([][]byte, error)) error {
	producer := playerAIProducer{produce: produce}
	err := e.packet.ScheduleFunc(delay, producer.run)
	if err != nil {
		return fmt.Errorf("aiScheduleFunc: %w", err)
	}
	return nil
}

func playerAIProducers(producers []raknet.ScheduledPacketProducer) []raknet.ScheduledPacketProducer {
	filteredProducers := make([]raknet.ScheduledPacketProducer, len(producers))
	for index, scheduled := range producers {
		if scheduled.Produce != nil {
			producer := playerAIProducer{produce: scheduled.Produce}
			scheduled.Produce = producer.run
		}
		filteredProducers[index] = scheduled
	}
	return filteredProducers
}

func (e playerAISchedule) group(producers []raknet.ScheduledPacketProducer) (raknet.CancelSchedule, error) {
	cancel, err := e.packet.ScheduleGroup(playerAIProducers(producers))
	if err != nil {
		return nil, fmt.Errorf("aiScheduleGroup: %w", err)
	}
	return cancel, nil
}

func (e playerAISchedule) groupResult(producers []raknet.ScheduledPacketProducer, complete func(error)) (raknet.CancelSchedule, error) {
	cancel, err := e.packet.ScheduleGroupResult(playerAIProducers(producers), complete)
	if err != nil {
		return nil, fmt.Errorf("aiScheduleResult: %w", err)
	}
	return cancel, nil
}
