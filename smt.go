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

func (tree *BASSparseMerkleTree) Get(key []byte, version *Version) ([]byte, error) {
	return nil, nil
}

func (tree *BASSparseMerkleTree) Set(key, val []byte) {
}

func (tree *BASSparseMerkleTree) IsEmpty(key []byte) bool {
	return false
}

func (tree *BASSparseMerkleTree) Root() []byte {
	return nil
}

func (tree *BASSparseMerkleTree) GetProof(key []byte, version *Version) (Proof, error) {
	return Proof{}, nil
}

func (tree *BASSparseMerkleTree) LatestVersion() Version {
	return 0
}

func (tree *BASSparseMerkleTree) Reset() error {
	return nil
}

func (tree *BASSparseMerkleTree) Commit() (Version, error) {
	return 0, nil
}

func (tree *BASSparseMerkleTree) Rollback(version Version) error {
	return nil
}
