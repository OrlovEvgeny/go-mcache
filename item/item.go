package item

import "time"

//
type Item struct {
	Key      string
	Expire   time.Time
	Data     []byte
	DataLink interface{}
}

// check expire cache
func (i Item) IsExpire() bool {
	return i.Expire.Before(time.Now().Local())
}
