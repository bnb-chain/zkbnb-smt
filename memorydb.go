package bsmt

var _ TreeDB = (*MemoryDB)(nil)

// MemoryDB is a key-value store.
type MemoryDB struct{}

func (db *MemoryDB) Get(key []byte) ([]byte, error)     { return nil, nil }
func (db *MemoryDB) Has(key []byte) (bool, error)       { return false, nil }
func (db *MemoryDB) Set(key []byte, value []byte) error { return nil }
func (db *MemoryDB) Delete(key []byte) error            { return nil }
func (db *MemoryDB) NewBatch() Batcher                  { return nil }
