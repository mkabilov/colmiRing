package main

import (
	"fmt"

	"tinygo.org/x/bluetooth"

	"github.com/mkabilov/colmiRing/pkg/colmi"
)

const (
	darwinAddr = "<insert your device address here (e.g. 12345678-b13a-7b74-e878-445cb7fc95b5)>"
	linuxAddr  = "<insert your device here too (e.g. 12:34:45:67:e5:04)>"
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

	heartbeatLog, keys, err := ring.HeartRateLog()
	if err != nil {
		panic(err)
	}
	for _, key := range keys {
		fmt.Printf("%v: %d\n", key, heartbeatLog[key])
	}
}
