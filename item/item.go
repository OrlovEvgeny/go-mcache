package item

import "time"

//
type Item struct {
	Key      string
	Expire   time.Time
	Data     []byte
	DataLink interface{}
}
