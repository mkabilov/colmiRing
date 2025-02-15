package colmi

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	btScanTimeout    = 30 * time.Second
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

	for i := range addrs {
		addrs[i] = strings.ToLower(addrs[i])
	}

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

func (c *Ring) heartrateLogPacket() []byte {
	// the sequence is to fetch the data from the very beginning of the ring log
	// supposedly it is the timestamp from which fetch data from
	return makePacket(cmdHeartRateLog, []byte{0x00, 0xD9, 0xAF, 0x67})
}

func (c *Ring) BlinkTwice() error {
	if err := c.Write(c.blinkTwicePacket()); err != nil {
		return fmt.Errorf("could not write command: %v", err)
	}

	return nil
}

func (c *Ring) processData(data []byte) {
	log.Printf("received data: %v", data)

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
		case 1:
			c.chunks <- newHeartRateLogStartTime(data)
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
		if c.currentChunkID == expectedHeartRateLogEntries {
			c.chunks <- HeartRateLogEnd{}
		}
	}
}

func (c *Ring) heartLogProcessor(ctx context.Context, heartbeatLog *map[time.Time]int, done chan struct{}) {
	bufChunks := [40][]byte{}
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
				bufChunks = [40][]byte{}
				clear(*heartbeatLog)
			case heartRateLogStartTime:
				startTime = time.Time(v)
			case heartRateLogInterval:
				intervalMin = int(v)
			case heartRateLogEntry:
				bufChunks[v.idx] = v.seq
			case HeartRateLogEnd:
				break ForLoop
			}
		case <-ctx.Done():
			return
		}
	}

	offset := time.Duration(0)
	for i := 0; i < 23; i++ {
		for _, v := range bufChunks[i] {
			(*heartbeatLog)[startTime.Add(offset)] = int(v)
			offset += time.Duration(intervalMin) * time.Minute
		}
	}
	close(done)
}

func (c *Ring) HeartRateLog() (map[time.Time]int, []time.Time, error) {
	heartbeatLog := make(map[time.Time]int)
	timestamps := make([]time.Time, 0)

	c.currentOperation = cmdHeartRateLog
	c.firstPacketReceived = false
	ctx, cancel := context.WithTimeout(context.Background(), logsFetchTimeout)
	defer cancel()
	doneCh := make(chan struct{})

	go c.heartLogProcessor(ctx, &heartbeatLog, doneCh)

	if err := c.Write(c.heartrateLogPacket()); err != nil {
		return heartbeatLog, timestamps, fmt.Errorf("could not write command: %v", err)
	}

	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("timeout fetching logs")
	case <-doneCh:
	}

	ts := make([]time.Time, 0)
	for t, _ := range heartbeatLog {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool {
		return ts[i].Before(ts[j])
	})

	return heartbeatLog, ts, nil
}
