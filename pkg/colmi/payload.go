package colmi

import "time"

type payload interface {
	data() // some placeholder method not to use the empty interface :-/
}

type heartRateLogInterval int

func (h heartRateLogInterval) data() {

}

type heartRateLogStartTime time.Time

func (h heartRateLogStartTime) data() {

}

func newHeartRateLogStartTime(resp []byte) heartRateLogStartTime {
	return heartRateLogStartTime(bytesToDate(resp))
}

type heartRateLogEntry struct {
	idx int
	seq []byte
}

func (h heartRateLogEntry) data() {
}

type HeartRateLogStart struct{}

func (h HeartRateLogStart) data() {
}

type HeartRateLogEnd struct{}

func (h HeartRateLogEnd) data() {
}
