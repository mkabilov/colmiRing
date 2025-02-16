package main

import (
	"fmt"
	"time"

	"tinygo.org/x/bluetooth"

	"github.com/mkabilov/colmiRing/pkg/colmi"
)

const (
	darwinAddr = "4b917133-b13a-7b74-e878-445cb7fc95b5"
	linuxAddr  = "30:31:44:36:e5:04"
)

func main() {
	ring := colmi.NewRing(bluetooth.DefaultAdapter)
	if err := ring.Init(); err != nil {
		panic(err)
	}

	// 2 device addresses are not necessary. you can specify one
	if addr, err := ring.Scan(darwinAddr, linuxAddr); err != nil {
		panic(err)
	} else if err = ring.Connect(addr); err != nil {
		panic(err)
	}

	heartbeatLog, keys, err := ring.HeartRateLog(time.Now())
	if err != nil {
		panic(err)
	}
	for _, key := range keys {
		fmt.Printf("%v: %d\n", key, heartbeatLog[key])
	}

	ring.Disconnect()
}
