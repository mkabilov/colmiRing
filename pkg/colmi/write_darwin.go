package colmi

import (
	"fmt"
	"log"
)

func (c *Ring) Write(data []byte) error {
	log.Printf("sent: >>>%v", data)
	if _, err := c.rxCh.Write(data); err != nil {
		return fmt.Errorf("could not write data: %v", err)
	}

	return nil
}
