package mcache

import (
	"log"
	"testing"
)

var (
	mcache  *CacheDriver
	dataSet = new(TestData)
	key1    = "keystr1"
	key2    = "keystr2"
)

//TestData
type TestData struct {
	ID   int
	Name string
	Age  int
}

//Set cache Pointer data
func TestSet(t *testing.T) {
	err := mcache.Set(key2, dataSet, TTL_FOREVER)
	if err != nil {
		t.Errorf("Error %s cache data: %v, ERROR_MSG: %v", t.Name(), dataSet, err)
	}
	log.Printf("%s : OK\n", t.Name())
}

//Get cache Pointer
func TestGetPointer(t *testing.T) {
	if pointer, ok := mcache.Get(key2); ok {
		if obj, ok := pointer.(*TestData); ok {
			if obj.Age != dataSet.Age {
				t.Errorf("Cache data incorrect by key: %s", key2)
			}
		}
	} else {
		t.Errorf("Cache not found by key: %s", key2)
	}
	log.Printf("%s : OK\n", t.Name())
}

//Get Len cache
func TestLen(t *testing.T) {
	if mcache.Len() != 2 {
		t.Errorf("Cache %s incorrect by key", t.Name())
	}
	log.Printf("%s : OK\n", t.Name())
}

//Remove two key
func TestRemove(t *testing.T) {
	mcache.Remove(key2)
	mcache.Remove(key1)
	if mcache.Len() != 0 {
		t.Errorf("Cache %s incorrect", t.Name())
	}
	log.Printf("%s : OK\n", t.Name())

}

//TestClose
func TestClose(t *testing.T) {
	mcache.Close()
	log.Printf("%s : OK\n", t.Name())
}
