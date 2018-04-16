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
func IsExpire(t time.Time) bool {
	return t.Before(time.Now().Local())
}
