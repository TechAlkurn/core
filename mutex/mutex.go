package mutex

import "sync"

type mutex struct {
	mu    sync.Mutex
	store map[string]any
}

func NewMutex() *mutex {
	return &mutex{
		store: make(map[string]any),
	}
}

func (c *mutex) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.store[key]
	return value, ok
}

func (c *mutex) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
}

func (c *mutex) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *mutex) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *mutex) Has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.store[key]
	return ok
}

func (c *mutex) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.store))
	for key := range c.store {
		keys = append(keys, key)
	}
	return keys
}

func (c *mutex) Values() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	values := make([]any, 0, len(c.store))
	for _, value := range c.store {
		values = append(values, value)
	}
	return values
}

func (c *mutex) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.store)
}

func (c *mutex) Range(f func(key string, value any) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range c.store {
		if !f(key, value) {
			break
		}
	}
}

func (c *mutex) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]any)
}

func (c *mutex) SetDefault(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
}

func (c *mutex) GetDefault(key string, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if value, ok := c.store[key]; ok {
		return value
	}
	return defaultValue
}

func (c *mutex) GetOrSet(key string, value any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
	}
	return c.store[key]
}

func (c *mutex) GetOrSetDefault(key string, value any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = value
		return value
	}
	return c.store[key]
}

func (c *mutex) GetOrSetFunc(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *mutex) GetOrSetFuncDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}

func (c *mutex) GetOrSetFuncLock(key string, f func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
	}
	return c.store[key]
}

func (c *mutex) GetOrSetFuncLockDefault(key string, f func() any, defaultValue any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.store[key]; !ok {
		c.store[key] = f()
		return f()
	}
	return c.store[key]
}

type rwMutex struct {
	mu    sync.Mutex
	store map[string]any
}
