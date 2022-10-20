// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package redis

import (
	"bytes"
	"context"
	"sync"

	"github.com/go-redis/redis/v8"
	stdErrors "github.com/pkg/errors"

	"github.com/bnb-chain/zkbnb-smt/database"
	"github.com/bnb-chain/zkbnb-smt/utils"
)

var (
	_ database.TreeDB  = (*Database)(nil)
	_ database.Batcher = (*batch)(nil)
)

// New returns a wrapped Redis object.
func New(config *RedisConfig, opts ...Option) (*Database, error) {
	var client RedisClient
	if len(config.ClusterAddr) > 0 {
		// cluster mode
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:              config.ClusterAddr,
			PoolSize:           config.PoolSize,
			Username:           config.Username,
			Password:           config.Password,
			MaxRedirects:       config.MaxRedirects,
			ReadOnly:           config.ReadOnly,
			RouteByLatency:     config.RouteByLatency,
			RouteRandomly:      config.RouteRandomly,
			MaxRetries:         config.MaxRetries,
			MinRetryBackoff:    config.MinRetryBackoff,
			MaxRetryBackoff:    config.MaxRetryBackoff,
			DialTimeout:        config.DialTimeout,
			ReadTimeout:        config.ReadTimeout,
			WriteTimeout:       config.WriteTimeout,
			MinIdleConns:       config.MinIdleConns,
			MaxConnAge:         config.MaxConnAge,
			PoolFIFO:           config.PoolFIFO,
			PoolTimeout:        config.PoolTimeout,
			IdleTimeout:        config.IdleTimeout,
			IdleCheckFrequency: config.IdleCheckFrequency,
		})
	} else {
		// single node mode
		client = redis.NewClient(&redis.Options{
			Addr:               config.Addr,
			PoolSize:           config.PoolSize,
			Username:           config.Username,
			Password:           config.Password,
			MaxRetries:         config.MaxRetries,
			MinRetryBackoff:    config.MinRetryBackoff,
			MaxRetryBackoff:    config.MaxRetryBackoff,
			DialTimeout:        config.DialTimeout,
			ReadTimeout:        config.ReadTimeout,
			WriteTimeout:       config.WriteTimeout,
			MinIdleConns:       config.MinIdleConns,
			MaxConnAge:         config.MaxConnAge,
			PoolFIFO:           config.PoolFIFO,
			PoolTimeout:        config.PoolTimeout,
			IdleTimeout:        config.IdleTimeout,
			IdleCheckFrequency: config.IdleCheckFrequency,
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.DialTimeout)
	defer cancel()
	err := client.Ping(ctx).Err()
	if err != nil {
		return nil, err
	}
	db := &Database{
		db: client,
	}

	for _, opt := range opts {
		opt.Apply(db)
	}

	return db, nil
}

// NewFromExistRedisClient returns a wrapped Redis object.
func NewFromExistRedisClient(client RedisClient, opts ...Option) *Database {
	db := &Database{
		db: client,
	}
	for _, opt := range opts {
		opt.Apply(db)
	}
	return db
}

// WrapWithNamespace returns a wrapped Redis object.
// The namespace is the prefix that the datastore.
func WrapWithNamespace(db *Database, namespace string) *Database {
	return &Database{
		namespace:  []byte(namespace),
		db:         db.db,
		sharedPipe: db.sharedPipe,
	}
}

type Database struct {
	namespace  []byte
	db         RedisClient // redis client
	sharedPipe redis.Pipeliner
}

// wrapKey returns a wrapper key with namespace.
func wrapKey(namespace, key []byte) string {
	if len(namespace) > 0 {
		return utils.BytesToString((bytes.Join([][]byte{namespace, key}, []byte(":"))))
	}
	return utils.BytesToString(key)
}

// Close flushes any pending data to disk and closes
// all io accesses to the underlying key-value store.
func (db *Database) Close() error {
	return db.db.Close()
}

// Has retrieves if a key is present in the key-value store.
func (db *Database) Has(key []byte) (bool, error) {
	dat, err := db.db.Exists(context.Background(), wrapKey(db.namespace, key)).Result()
	if err != nil {
		return false, err
	}
	return dat > 0, nil
}

// Get retrieves the given key if it's present in the key-value store.
func (db *Database) Get(key []byte) ([]byte, error) {
	dat, err := db.db.Get(context.Background(), wrapKey(db.namespace, key)).Result()
	if err != nil && stdErrors.Is(redis.Nil, err) {
		return nil, database.ErrDatabaseNotFound
	}
	return utils.StringToBytes(dat), err
}

// Put inserts the given value into the key-value store.
func (db *Database) Set(key []byte, value []byte) error {
	return db.db.Set(context.Background(), wrapKey(db.namespace, key), value, 0).Err()
}

// Delete removes the key from the key-value store.
func (db *Database) Delete(key []byte) error {
	return db.db.Del(context.Background(), wrapKey(db.namespace, key)).Err()
}

// NewBatch creates a write-only key-value store that buffers changes to its host
// database until a final write is called.
func (db *Database) NewBatch() database.Batcher {
	pipe := db.sharedPipe
	if pipe == nil {
		pipe = db.db.Pipeline()
	}
	return &batch{
		db:        db.db,
		namespace: db.namespace,
		b:         pipe,
	}
}

// batch is a write-only leveldb batch that commits changes to its host database
// when Write is called. A batch cannot be used concurrently.
type batch struct {
	namespace []byte
	db        RedisClient
	b         redis.Pipeliner
	size      int
	lock      sync.RWMutex
}

// Put inserts the given value into the batch for later committing.
func (b *batch) Set(key, value []byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.b.Set(context.Background(), wrapKey(b.namespace, key), value, 0).Err()
	b.size += len(key) + len(value)
	return nil
}

// Delete inserts the a key removal into the batch for later committing.
func (b *batch) Delete(key []byte) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.b.Del(context.Background(), wrapKey(b.namespace, key))
	b.size += len(key)
	return nil
}

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	b.lock.Lock()
	defer b.lock.Unlock()
	if b.size == 0 {
		return nil
	}
	_, err := b.b.Exec(context.Background())
	if err != nil {
		return err
	}
	b.size = 0
	return nil
}

// ValueSize retrieves the amount of data queued up for writing.
func (b *batch) ValueSize() int {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.size
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.b.Discard()
	b.size = 0
}
