package colmi

import "fmt"

func (c *Ring) Write(data []byte) error {
	if _, err := c.rxCh.Write(data); err != nil {
		return fmt.Errorf("could not write data: %v", err)
	}

	return nil
}
