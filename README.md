# MCache library

[![Build Status](https://travis-ci.org/OrlovEvgeny/go-mcache.svg?branch=master)](https://travis-ci.org/OrlovEvgeny/go-mcache)
[![Go Report Card](https://goreportcard.com/badge/github.com/OrlovEvgeny/go-mcache?v1)](https://goreportcard.com/report/github.com/OrlovEvgeny/go-mcache)
[![GoDoc](https://godoc.org/github.com/OrlovEvgeny/go-mcache?status.svg)](https://godoc.org/github.com/OrlovEvgeny/go-mcache)

![MCache](logo.jpg)

go-mcache - this is a fast key:value storage.
Its major advantage is that, being essentially a thread-safe .

```go 
map[string]interface{}
``` 
with expiration times, it doesn't need to serialize, and quick removal of expired keys.

# Installation

```bash
~ $ go get -u github.com/OrlovEvgeny/go-mcache
```

**Example a Pointer value (vary fast method)**

```go
type User struct {
	Name string
	Age  uint
	Bio  string
}

func main() {
	//Start mcache instance
	MCache = mcache.StartInstance()

	//Create custom key
	key := "custom_key1"

	//Create example struct
	user := &User{
		Name: "John",
		Age:  20,
		Bio:  "gopher 80 lvl",
	}
	
	//args - key, &value, ttl
	err := MCache.SetPointer(key, user, time.Minute*20)
	if err != nil {
		log.Fatal(err)
	}

	if pointer, ok := MCache.GetPointer(key); ok {
		objUser:= pointer.(*User)
		fmt.Printf("User name: %s, Age: %d, Bio: %s\n", objUser.Name, objUser.Age, objUser.Bio)			
	}
}
```



**Example serialize and deserialize value** (slow method)

```go
type User struct {
	Name string
	Age  uint
	Bio  string
}

func main() {
	//Start mcache instance
	MCache = mcache.StartInstance()

	//Create custom key
	key := "custom_key2"

	userSet := &User{
		Name: "Bob",
		Age:  33,
		Bio:  "gopher 80 lvl",
	}
	//args - key, &value, ttl
	err := MCache.Set(key, userSet, time.Minute*20)
	if err != nil {
		log.Fatal(err)
	}

	var userGet User
	if ok := MCache.Get(key, &userGet); ok {
		fmt.Printf("User name: %s, Age: %d, Bio: %s\n", userGet.Name, userGet.Age, userGet.Bio)
	}
}
```


### Performance Benchmarks

    goos: darwin
    goarch: amd64
    BenchmarkWrite          200000              8706 ns/op
    BenchmarkRead          1000000              1589 ns/op
    BenchmarkRW             300000             12194 ns/op


*dependency use*: [msgpack](https://github.com/vmihailenco/msgpack)

### What should be done

- [x] the possibility of closing
- [x] r/w benchmark statistics
- [ ] rejection of channels in safeMap in favor of sync.Mutex (there is an opinion that it will be faster)




# License:

[MIT](LICENSE)
