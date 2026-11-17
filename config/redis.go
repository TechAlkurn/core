package config

import (
	"context"
	"os"

	"github.com/go-redis/redis/v8"
)

var (
	Ctx   = context.Background()
	Redis *redis.Client
)

func InitRedis() {
	Redis = redis.NewClient(&redis.Options{
		Addr: os.Getenv("REDIS_ADDR"),
		DB:   0, // use default DB
	})
}
