package safeMap

import entity "github.com/OrlovEvgeny/go-mcache/item"

//
type safeMap chan commandData

//
type commandAction int

//
type commandData struct {
	action commandAction
	key    string
	keys   []string
	value  interface{}
	result chan<- interface{}
	data   chan<- map[string]interface{}
}

//
const (
	REMOVE commandAction = iota
	FLUSH
	FIND
	INSERT
	COUNT
	TRUNCATE
	END
)

//
type findResult struct {
	value interface{}
	found bool
}

//
type SafeMap interface {
	Insert(string, interface{})
	Delete(string)
	Truncate()
	Flush([]string)
	Find(string) (interface{}, bool)
	Len() int
	Close() map[string]interface{}
}

//
func NewStorage() SafeMap {
	sm := make(safeMap)
	go sm.run()
	return sm
}

//
func (sm safeMap) run() {
	store := make(map[string]interface{})
	for command := range sm {
		switch command.action {
		case INSERT:
			store[command.key] = command.value
		case REMOVE:
			delete(store, command.key)
		case FLUSH:
			flush(store, command.keys)
		case FIND:
			value, found := store[command.key]
			command.result <- findResult{value, found}
		case COUNT:
			command.result <- len(store)
		case TRUNCATE:
			clearMap(store)
		case END:
			close(sm)
			command.data <- store
		}
	}
}

//
func (sm safeMap) Insert(key string, value interface{}) {
	sm <- commandData{action: INSERT, key: key, value: value}
}

//
func (sm safeMap) Delete(key string) {
	sm <- commandData{action: REMOVE, key: key}
}

//
func (sm safeMap) Flush(keys []string) {
	sm <- commandData{action: FLUSH, keys: keys}
}

//
func (sm safeMap) Find(key string) (value interface{}, found bool) {
	reply := make(chan interface{})
	sm <- commandData{action: FIND, key: key, result: reply}
	result := (<-reply).(findResult)
	return result.value, result.found
}

//
func (sm safeMap) Len() int {
	reply := make(chan interface{})
	sm <- commandData{action: COUNT, result: reply}
	return (<-reply).(int)
}

//
func (sm safeMap) Close() map[string]interface{} {
	reply := make(chan map[string]interface{})
	sm <- commandData{action: END, data: reply}
	return <-reply
}

//
func (sm safeMap) Truncate() {
	sm <- commandData{action: TRUNCATE}
}

//
func clearMap(store map[string]interface{}) {
	for k := range store {
		delete(store, k)
	}
}

//
func flush(s map[string]interface{}, keys []string) {
	for _, v := range keys {
		value, ok := s[v]
		if !ok {
			continue
		}
		if entity.IsExpire(value.(entity.Item).Expire) {
			delete(s, v)
		}
	}
}
