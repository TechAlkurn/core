package config

import (
	"sync"
	"time"

	"github.com/robfig/go-cache"
)

// var Lcache *cache.Cache

/*
	 func InitCache() {
		// Initialize the cache with expiration set to one day
		Lcache = cache.New(24*time.Hour, 48*time.Hour)
	}
*/
var (
	Lcache *cache.Cache
	once   sync.Once
)

func InitCache() *cache.Cache {
	once.Do(func() {
		Lcache = cache.New(24*time.Hour, 48*time.Hour)
	})
	return Lcache
}

var GetCache = func(k string) (any, bool) {
	return Lcache.Get(k)
}

var SetCache = func(k string, x any, d time.Duration) {
	Lcache.Set(k, x, d)
}

var DeleteCache = func(key string) {
	Lcache.Delete(key)
}

var ResetCache = func(key string) {
	Lcache.Delete(key)
}

var FlushAll = func() {
	Lcache.Flush()
}

var Flush = func(key string) {
	Lcache.Delete(key)
}

var Has = func(key string) bool {
	_, found := Lcache.Get(key)
	return found
}
