package colmi

import (
	"context"
	"fmt"
	"log"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	btScanTimeout    = 10 * time.Second
	btConnectTimeout = 10 * time.Second
	logsFetchTimeout = 10 * time.Second

	expectedHeartRateLogEntries = 23
)

// commands
const (
	cmdBlinkTwice           = 0x10 //
	cmdHeartRateLog         = 0x15
	cmdHeartRateLogSettings = 0x16
	cmdBattery              = 0x03
	cmdHeartRate            = 0x15
	cmdReboot               = 0x08
)

var (
	connParams = bluetooth.ConnectionParams{
		ConnectionTimeout: bluetooth.NewDuration(btConnectTimeout),
	}

	uartServiceUUID = bluetooth.NewUUID([16]byte{
		0x6e, 0x40, 0xff, 0xf0, 0xb5, 0xa3, 0xf3, 0x93,
		0xe0, 0xa9, 0xe5, 0x0e, 0x24, 0xdc, 0xca, 0x9e,
	})

	rxCharacteristicUUID = bluetooth.NewUUID([16]byte{
		0x6e, 0x40, 0x00, 0x02, 0xb5, 0xa3, 0xf3, 0x93,
		0xe0, 0xa9, 0xe5, 0x0e, 0x24, 0xdc, 0xca, 0x9e,
	})

	txCharacteristicUUID = bluetooth.NewUUID([16]byte{
		0x6e, 0x40, 0x00, 0x03, 0xb5, 0xa3, 0xf3, 0x93,
		0xe0, 0xa9, 0xe5, 0x0e, 0x24, 0xdc, 0xca, 0x9e,
	})
)

type Ring struct {
	currentOperation    byte
	firstPacketReceived bool
	chunks              chan payload
	currentChunkID      int //since command sent
	expectedChunks      *int

	btDev   bluetooth.Device
	adapter *bluetooth.Adapter

	// UART way of communication: tx->rx; rx->tx
	rxCh bluetooth.DeviceCharacteristic // writing data to
	txCh bluetooth.DeviceCharacteristic // reading data from
}

func NewRing(adapter *bluetooth.Adapter) *Ring {
	c := &Ring{
		adapter: adapter,
		chunks:  make(chan payload),
	}

	return c
}

func (c *Ring) Init() error {
	if err := c.adapter.Enable(); err != nil {
		return fmt.Errorf("could not enable adapter: %v", err)
	}

	return nil
}

func (c *Ring) Scan(addrs ...string) (bluetooth.Address, error) {
	var btAddr bluetooth.Address
	errCh := make(chan error)

	ctx, cancel := context.WithTimeout(context.Background(), btScanTimeout)
	defer cancel()

	go func() {
		err := c.adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
			addr := strings.ToLower(result.Address.String())
			if !slices.Contains(addrs, addr) {
				return
			}
			log.Printf("found ring with address: %v", addr)
			btAddr = result.Address
			if err := adapter.StopScan(); err != nil {
				log.Printf("stop scan problem: %v", err)
			}
			errCh <- nil
		})
		if err != nil {
			errCh <- fmt.Errorf("could not find ring: %v", err)
		}
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return btAddr, err
		}
	case <-ctx.Done():
		c.adapter.StopScan()
		return btAddr, fmt.Errorf("timeout scanning")
	}
	close(errCh)

	return btAddr, nil
}

func (c *Ring) Connect(addr bluetooth.Address) error {
	var err error

	if c.btDev, err = c.adapter.Connect(addr, connParams); err != nil {
		return fmt.Errorf("could not connect to ring(addr: %q): %v", addr, err)
	}
	log.Printf("connected to the ring with %q address", addr)

	srvS, err := c.btDev.DiscoverServices([]bluetooth.UUID{uartServiceUUID})
	if err != nil {
		return fmt.Errorf("could not discover services: %s", err)
	}
	if ln := len(srvS); ln == 0 {
		return fmt.Errorf("no services found")
	} else if ln != 1 {
		return fmt.Errorf("wrong number of services: %d, expected 1", ln)
	}

	dvcCh, err := srvS[0].DiscoverCharacteristics([]bluetooth.UUID{rxCharacteristicUUID, txCharacteristicUUID})
	if err != nil {
		return fmt.Errorf("could not discover characteristics: %s", err)
	}
	var rxSet, txSet bool
	for _, ch := range dvcCh {
		uuid := ch.UUID()
		switch uuid {
		case rxCharacteristicUUID:
			c.rxCh = ch
			rxSet = true
		case txCharacteristicUUID:
			c.txCh = ch
			txSet = true
		default:
			log.Printf("unknown characteristic %v, skipping", uuid)
		}
	}

	if !rxSet || !txSet {
		return fmt.Errorf("could not bind characteristics")
	}

	if err := c.txCh.EnableNotifications(c.processData); err != nil {
		return fmt.Errorf("could not enable processing of the incoming data: %v", err)
	}

	return nil
}

func (c *Ring) Disconnect() error {
	if err := c.btDev.Disconnect(); err != nil {
		return fmt.Errorf("could not disconnect from ring: %v", err)
	}
	log.Printf("disconnected from the ring")

	return nil
}

func (c *Ring) blinkTwicePacket() []byte {
	return makePacket(cmdBlinkTwice, []byte{})
}

func (c *Ring) heartrateLogPacket(startTime time.Time) []byte {
	return makePacket(cmdHeartRateLog, dateToBytes(truncateToDay(startTime)))
}

func (c *Ring) BlinkTwice() error {
	if err := c.Write(c.blinkTwicePacket()); err != nil {
		return fmt.Errorf("could not write command: %v", err)
	}

	return nil
}

func (c *Ring) processData(data []byte) {
	log.Printf("data: %v", data)

	switch c.currentOperation {
	case cmdHeartRateLog:
		if data[0] != cmdHeartRateLog {
			break
		}

		if !c.firstPacketReceived {
			c.currentChunkID = 0
			c.firstPacketReceived = true
			c.chunks <- HeartRateLogStart{}
		} else {
			c.currentChunkID++
		}

		hbIndex := data[1]
		switch hbIndex {
		case 0:
			c.chunks <- heartRateLogInterval(data[3])
			chunks := int(data[2]) - 1 // excluding this chunk
			c.expectedChunks = &chunks
		case 1:
			c.chunks <- newHeartRateLogStartTime(data[2:6])
			c.chunks <- heartRateLogEntry{
				idx: 1,
				seq: data[6:15],
			}
		default:
			c.chunks <- heartRateLogEntry{
				idx: int(hbIndex),
				seq: data[2:15],
			}
		}
		if !(c.expectedChunks == nil || c.currentChunkID < *c.expectedChunks) {
			c.chunks <- HeartRateLogEnd{}
		}
	}
}

func (c *Ring) heartLogProcessor(ctx context.Context, heartbeatLog *map[time.Time]int, done chan struct{}) {
	bufChunks := make(map[int][]byte)
	maxChunkID := 0
	var (
		startTime   time.Time
		intervalMin int
	)

ForLoop:
	for {
		select {
		case chunk := <-c.chunks:
			switch v := chunk.(type) {
			case HeartRateLogStart:
				clear(bufChunks)
				clear(*heartbeatLog)
			case heartRateLogStartTime:
				startTime = time.Time(v)
			case heartRateLogInterval:
				intervalMin = int(v)
			case heartRateLogEntry:
				bufChunks[v.idx] = v.seq
				maxChunkID = int(math.Max(float64(maxChunkID), float64(v.idx)))
			case HeartRateLogEnd:
				break ForLoop
			}
		case <-ctx.Done():
			return
		}
	}

	offset := time.Duration(0)
	for i := 0; i <= maxChunkID; i++ {
		for _, v := range bufChunks[i] {
			(*heartbeatLog)[startTime.Add(offset)] = int(v)
			offset += time.Duration(intervalMin) * time.Minute
		}
	}
	close(done)
}

func (c *Ring) HeartRateLog(startTime time.Time) (map[time.Time]int, []time.Time, error) {
	heartbeatLog := make(map[time.Time]int)

	c.currentOperation = cmdHeartRateLog
	c.firstPacketReceived = false
	ctx, cancel := context.WithTimeout(context.Background(), logsFetchTimeout)
	defer cancel()
	doneCh := make(chan struct{})

	go c.heartLogProcessor(ctx, &heartbeatLog, doneCh)

	if err := c.Write(c.heartrateLogPacket(startTime)); err != nil {
		return nil, nil, err
	}

	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("timeout fetching logs")
	case <-doneCh:
	}

	ts := make([]time.Time, 0)
	for t := range heartbeatLog {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool {
		return ts[i].Before(ts[j])
	})

	return heartbeatLog, ts, nil
}
