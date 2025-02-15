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
	val := int64(0)
	val += int64(resp[5]) * 16777216
	val += int64(resp[4]) * 65536
	val += int64(resp[3]) * 256
	val += int64(resp[2])

	return heartRateLogStartTime(time.Unix(val, 0).UTC())
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
