package mutex

import "sync"

type rWMutex struct {
	mu    sync.RWMutex
	store map[string]any
}

func NewRWMutex() *rWMutex {
	return &rWMutex{
		store: make(map[string]any),
	}
}

func (c *rWMutex) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.store[key]
	return value, ok
}

func (c *rWMutex) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
}

func (c *rWMutex) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *rWMutex) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *rWMutex) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.store[key]
	return ok
}

func (c *rWMutex) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, 0, len(c.store))
	for key := range c.store {
		keys = append(keys, key)
	}
	return keys
}

func (c *rWMutex) Values() []any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	values := make([]any, 0, len(c.store))
	for _, value := range c.store {
		values = append(values, value)
	}
	return values
}

func (c *rWMutex) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.store)
}

func (c *rWMutex) Range(f func(key string, value any) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for key, value := range c.store {
		if !f(key, value) {
			break
		}
	}
}

func (c *rWMutex) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *rWMutex) SetDefault(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
}

func (c *rWMutex) GetDefault(key string, defaultValue any) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if value, ok := c.store[key]; ok {
		return value
	}
	return defaultValue
}

func (c *rWMutex) GetOrSet(key string, value any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
	return c.store[key]
}

func (c *rWMutex) GetOrSetDefault(key string, value any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
		return value
	}
	return c.store[key]
}

func (c *rWMutex) GetOrSetFunc(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *rWMutex) GetOrSetFuncDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}

func (c *rWMutex) GetOrSetFuncLock(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *rWMutex) GetOrSetFuncLockDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}
