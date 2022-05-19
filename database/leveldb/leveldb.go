package leveldb

import (
	"sync"

	"github.com/bnb-chain/bas-smt/database"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

var (
	_ database.TreeDB  = (*Database)(nil)
	_ database.Batcher = (*batch)(nil)
)

const (
	// minCache is the minimum amount of memory in megabytes to allocate to leveldb
	// read and write caching, split half and half.
	minCache = 16

	// minHandles is the minimum number of files handles to allocate to the open
	// database files.
	minHandles = 16
)

type Database struct {
	fn        string // filename for reporting
	namespace []byte
	db        *leveldb.DB     // LevelDB instance
	quitLock  sync.Mutex      // Mutex protecting the quit channel access
	quitChan  chan chan error // Quit channel to stop the metrics collection before closing the database
}

// New returns a wrapped LevelDB object. The namespace is the prefix that the
// metrics reporting should use for surfacing internal stats.
func New(file string, cache int, handles int, namespace string, readonly bool) (*Database, error) {
	return NewCustom(file, namespace, func(options *opt.Options) {
		// Ensure we have some minimal caching and file guarantees
		if cache < minCache {
			cache = minCache
		}
		if handles < minHandles {
			handles = minHandles
		}
		// Set default options
		options.OpenFilesCacheCapacity = handles
		options.BlockCacheCapacity = cache / 2 * opt.MiB
		options.WriteBuffer = cache / 4 * opt.MiB // Two of these are used internally
		if readonly {
			options.ReadOnly = true
		}
	})
}

// NewCustom returns a wrapped LevelDB object. The namespace is the prefix that the
// metrics reporting should use for surfacing internal stats.
// The customize function allows the caller to modify the leveldb options.
func NewCustom(file string, namespace string, customize func(options *opt.Options)) (*Database, error) {
	options := configureOptions(customize)

	// Open the db and recover any potential corruptions
	db, err := leveldb.OpenFile(file, options)
	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		db, err = leveldb.RecoverFile(file, nil)
	}
	if err != nil {
		return nil, err
	}

	ldb := &Database{
		fn:        file,
		namespace: []byte(namespace),
		db:        db,
		quitChan:  make(chan chan error),
	}
	return ldb, nil
}

// configureOptions sets some default options, then runs the provided setter.
func configureOptions(customizeFn func(*opt.Options)) *opt.Options {
	// Set default options
	options := &opt.Options{
		Filter:                 filter.NewBloomFilter(10),
		DisableSeeksCompaction: true,
	}
	// Allow caller to make custom modifications to the options
	if customizeFn != nil {
		customizeFn(options)
	}
	return options
}

// Close stops the metrics collection, flushes any pending data to disk and closes
// all io accesses to the underlying key-value store.
func (db *Database) Close() error {
	db.quitLock.Lock()
	defer db.quitLock.Unlock()

	if db.quitChan != nil {
		errc := make(chan error)
		db.quitChan <- errc
		if err := <-errc; err != nil {
			return err
		}
		db.quitChan = nil
	}
	return db.db.Close()
}

// Has retrieves if a key is present in the key-value store.
func (db *Database) Has(key []byte) (bool, error) {
	return db.db.Has(append(db.namespace, key...), nil)
}

// Get retrieves the given key if it's present in the key-value store.
func (db *Database) Get(key []byte) ([]byte, error) {
	dat, err := db.db.Get(append(db.namespace, key...), nil)
	if err != nil {
		return nil, err
	}
	return dat, nil
}

// Put inserts the given value into the key-value store.
func (db *Database) Set(key []byte, value []byte) error {
	return db.db.Put(append(db.namespace, key...), value, nil)
}

// Delete removes the key from the key-value store.
func (db *Database) Delete(key []byte) error {
	return db.db.Delete(append(db.namespace, key...), nil)
}

// NewBatch creates a write-only key-value store that buffers changes to its host
// database until a final write is called.
func (db *Database) NewBatch() database.Batcher {
	return &batch{
		db:        db.db,
		namespace: db.namespace,
		b:         new(leveldb.Batch),
	}
}

// batch is a write-only leveldb batch that commits changes to its host database
// when Write is called. A batch cannot be used concurrently.
type batch struct {
	namespace []byte
	db        *leveldb.DB
	b         *leveldb.Batch
}

// Put inserts the given value into the batch for later committing.
func (b *batch) Set(key, value []byte) error {
	b.b.Put(append(b.namespace, key...), value)
	return nil
}

// Delete inserts the a key removal into the batch for later committing.
func (b *batch) Delete(key []byte) error {
	b.b.Delete(append(b.namespace, key...))
	return nil
}

// Write flushes any accumulated data to disk.
func (b *batch) Write() error {
	return b.db.Write(b.b, nil)
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.b.Reset()
}
