# MCache library (Beta)

[![Go Report Card](https://goreportcard.com/badge/github.com/OrlovEvgeny/go-mcache)](https://goreportcard.com/report/github.com/OrlovEvgeny/go-mcache)
[![GoDoc](https://godoc.org/github.com/OrlovEvgeny/go-mcache?status.svg)](https://godoc.org/github.com/OrlovEvgeny/go-mcache)

go-mcache - this is a fast key:value storage
Its major advantage is that, being essentially a thread-safe 
```go 
map[string]interface{}
``` 
with expiration times, it doesn't need to serialize, and quick removal of expired keys

# Installation

```bash
~ $ go get github.com/OrlovEvgeny/go-mcache
```



**Example a Pointer value (vary fast method)**

```go
var MCache *mcache.CacheDriver

type User {
	Name string
	Age uint
	Bio string
}

func main() {
	MCache = mcache.StartInstance()
	
	key := "key1"
	
	user := new(User)
	user.Name = "John"
	user.Age = 20
	user.Bio = "gopher"
	//args - key, &value, ttl
	MCache.SetPointer(key, user, time.Minute*20)
	if err != nil {
		log.Println("MCACHE SET ERROR: ", err)
	}
	
	if pointer, ok := mcache.GetPointer(key); ok {
		if objUser, ok := pointer.(*User); ok {
			fmt.Printf("User name: %s, Age: %d, Bio: %s\n", objUser.Name, objUser.Age, objUser,Bio)
		}
	} else {
		log.Println("Cache by key: %s not found", key)
	}
}
```



**Example serialize and deserialize value**
```go
var MCache *mcache.CacheDriver

type User {
	Name string
	Age uint
	Bio string
}

func main() {
	MCache = mcache.StartInstance()
	
	key := "key1"
	
	userSet := new(User)
	userSet.Name = "John"
	userSet.Age = 20
	userSet.Bio = "gopher"
	
	//args - key, &value, ttl
	MCache.Set(key, userSet, time.Minute*20)
	if err != nil {
		log.Println("MCACHE SET ERROR: ", err)
	}
	
	
	var userGet User
	if ok := mcache.Get(key, &userGet); ok {
            fmt.Printf("User name: %s, Age: %d, Bio: %s\n", userGet.Name, userGet.Age, userGet,Bio)
    } else {
    	log.Println("Cache by key: %s not found", key)
    }
}
```


*dependency use*: [msgpack](https://github.com/vmihailenco/msgpack)


### What should be done

- [ ] the possibility of closing
- [ ] r/w benchmark statistics
- [ ] rejection of channels in safeMap in favor of sync.Mutex (there is an opinion that it will be faster)



# License:

[MIT](LICENSE)
