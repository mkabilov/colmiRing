package colmi

import "time"

func checkSum(data []byte) byte {
	var sum int
	for _, b := range data {
		sum += int(b)
	}

	return byte(sum % 256)
}

func makePacket(command byte, subData []byte) []byte {
	result := make([]byte, 16)
	result[0] = command
	for i, b := range subData {
		result[i+1] = b
	}
	result[15] = checkSum(result[:15])

	return result
}

func dateToBytes(t time.Time) []byte {
	t = t.UTC()
	return []byte{
		byte(t.Unix() & 0xFF),
		byte((t.Unix() >> 8) & 0xFF),
		byte((t.Unix() >> 16) & 0xFF),
		byte((t.Unix() >> 24) & 0xFF),
	}
}

func bytesToDate(d []byte) time.Time {
	val := int64(0)
	val += int64(d[3]) * 16777216
	val += int64(d[2]) * 65536
	val += int64(d[1]) * 256
	val += int64(d[0])

	return time.Unix(val, 0).UTC()
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.UTC().Location())
}
