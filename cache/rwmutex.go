package cache

import "sync"

type rWMutexCache struct {
	mu    sync.RWMutex
	store map[string]any
}

func NewRWMutexCache() *rWMutexCache {
	return &rWMutexCache{
		store: make(map[string]any),
	}
}

func (c *rWMutexCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.store[key]
	return value, ok
}

func (c *rWMutexCache) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
}

func (c *rWMutexCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *rWMutexCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *rWMutexCache) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.store[key]
	return ok
}

func (c *rWMutexCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, 0, len(c.store))
	for key := range c.store {
		keys = append(keys, key)
	}
	return keys
}

func (c *rWMutexCache) Values() []any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	values := make([]any, 0, len(c.store))
	for _, value := range c.store {
		values = append(values, value)
	}
	return values
}

func (c *rWMutexCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.store)
}

func (c *rWMutexCache) Range(f func(key string, value any) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for key, value := range c.store {
		if !f(key, value) {
			break
		}
	}
}

func (c *rWMutexCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *rWMutexCache) SetDefault(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
}

func (c *rWMutexCache) GetDefault(key string, defaultValue any) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if value, ok := c.store[key]; ok {
		return value
	}
	return defaultValue
}

func (c *rWMutexCache) GetOrSet(key string, value any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
	return c.store[key]
}

func (c *rWMutexCache) GetOrSetDefault(key string, value any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
		return value
	}
	return c.store[key]
}

func (c *rWMutexCache) GetOrSetFunc(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *rWMutexCache) GetOrSetFuncDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}

func (c *rWMutexCache) GetOrSetFuncLock(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *rWMutexCache) GetOrSetFuncLockDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}
