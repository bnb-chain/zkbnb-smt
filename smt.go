package bsmt

const (
	latestVersionKeyPrefix string = "latestVersion"
	recentVersionNumber    string = "recentVersionNumber"
	maxDepthKeyPrefix      string = "maxDepth"
)

var _ SparseMerkleTree = (*BASSparseMerkleTree)(nil)

func NewBASSparseMerkleTree(opts ...Option) SparseMerkleTree {
	smt := &BASSparseMerkleTree{}
	for _, opt := range opts {
		opt(smt)
	}
	return smt
}

type BASSparseMerkleTree struct {
	version       uint64
	root          *TreeNode // The working root node
	lastSavedRoot *TreeNode // The most recently saved root node

	proofsBefore []Proof
	db           TreeDB
}

func (tree BASSparseMerkleTree) Get(key []byte, version *Version) ([]byte, error) {
	return nil, nil
}

func (tree BASSparseMerkleTree) Set(key, val []byte) {
}

func (tree BASSparseMerkleTree) Reset() error {
	return nil
}

func (tree BASSparseMerkleTree) Commit() error {
	return nil
}

func (tree BASSparseMerkleTree) Rollback(version Version) error {
	return nil
}
