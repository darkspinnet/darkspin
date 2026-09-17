package horde

type WaveSequence struct {
	wave      int
	waveCount int
}

func NewWaveSequence(waveCount int, completedWave int) *WaveSequence {
	waveCount = max(0, waveCount)
	return &WaveSequence{
		wave:      min(max(0, completedWave), waveCount),
		waveCount: waveCount,
	}
}

func (e *WaveSequence) Current() int {
	if e == nil {
		return 0
	}
	return e.wave
}

func (e *WaveSequence) Next(isCurrentWaveClear bool) (int, bool) {
	if e == nil || !isCurrentWaveClear || e.wave >= e.waveCount {
		return e.Current(), false
	}
	e.wave++
	return e.wave, true
}

func (e *WaveSequence) IsComplete(isCurrentWaveClear bool) bool {
	return e != nil && isCurrentWaveClear && e.wave == e.waveCount
}
