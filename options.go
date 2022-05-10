package bsmt

// Option is a function that configures SMT.
type Option func(*BASSparseMerkleTree)

func WithCustomDB(db TreeDB) Option {
	return func(smt *BASSparseMerkleTree) {
		smt.db = db
	}
}
