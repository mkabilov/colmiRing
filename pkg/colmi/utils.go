package colmi

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
