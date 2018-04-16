package go_mcache

import (
	"log"
	"testing"
	"time"
)

var (
	mcache  *CacheDriver
	dataSet = new(TestData)
	key1    = "keystr1"
	key2    = "keystr2"
)

type TestData struct {
	ID   int
	Name string
	Age  int
}

//Set cache
func TestSet(t *testing.T) {
	mcache = StartInstance()

	dataSet.Name = "John"
	dataSet.ID = 11
	dataSet.Age = 24

	err := mcache.Set(key1, dataSet, time.Minute*2)
	if err != nil {
		t.Errorf("Error %s cache data: %v, ERROR_MSG: %v", t.Name(), dataSet, err)
	}
	log.Printf("%s : OK\n", t.Name())
}

//Get cache
func TestGet(t *testing.T) {
	var dataGet TestData
	if ok := mcache.Get(key1, &dataGet); ok {
		if dataGet.Name != dataSet.Name {
			t.Errorf("Cache data incorrect by key: %s", key1)
		}
	} else {
		t.Errorf("Cache not found by key: %s", key1)
	}
	log.Printf("%s : OK\n", t.Name())

}

//Set cache Pointer data
func TestSetPointer(t *testing.T) {
	err := mcache.SetPointer(key2, dataSet, time.Minute*2)
	if err != nil {
		t.Errorf("Error %s cache data: %v, ERROR_MSG: %v", t.Name(), dataSet, err)
	}
	log.Printf("%s : OK\n", t.Name())
}

//Get cache Pointer
func TestGetPointer(t *testing.T) {
	if pointer, ok := mcache.GetPointer(key2); ok {
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

func TestClose(t *testing.T) {
	mcache.Close()
	log.Printf("%s : OK\n", t.Name())
}
