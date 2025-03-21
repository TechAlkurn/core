package cache

import "sync"

type mutexCache struct {
	mu    sync.Mutex
	store map[string]any
}

func NewMutexCache() *mutexCache {
	return &mutexCache{
		store: make(map[string]any),
	}
}

func (c *mutexCache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.store[key]
	return value, ok
}

func (c *mutexCache) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
}

func (c *mutexCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *mutexCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *mutexCache) Has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.store[key]
	return ok
}

func (c *mutexCache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.store))
	for key := range c.store {
		keys = append(keys, key)
	}
	return keys
}

func (c *mutexCache) Values() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	values := make([]any, 0, len(c.store))
	for _, value := range c.store {
		values = append(values, value)
	}
	return values
}

func (c *mutexCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.store)
}

func (c *mutexCache) Range(f func(key string, value any) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range c.store {
		if !f(key, value) {
			break
		}
	}
}

func (c *mutexCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *mutexCache) SetDefault(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
}

func (c *mutexCache) GetDefault(key string, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if value, ok := c.store[key]; ok {
		return value
	}
	return defaultValue
}

func (c *mutexCache) GetOrSet(key string, value any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
	return c.store[key]
}

func (c *mutexCache) GetOrSetDefault(key string, value any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
		return value
	}
	return c.store[key]
}

func (c *mutexCache) GetOrSetFunc(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *mutexCache) GetOrSetFuncDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}

func (c *mutexCache) GetOrSetFuncLock(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *mutexCache) GetOrSetFuncLockDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}
