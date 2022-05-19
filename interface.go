package bsmt

type (
	Version          uint64
	SparseMerkleTree interface {
		Get(key []byte, version *Version) ([]byte, error)
		Set(key, val []byte) error
		IsEmpty(key []byte, version *Version) bool
		Root() []byte
		GetProof(key []byte, version *Version) (*Proof, error)
		VerifyProof(proof *Proof, version *Version) bool
		LatestVersion() Version
		Reset()
		Commit() (Version, error)
		Rollback(version Version) error
	}
	TreeNode interface {
		Root() []byte
		SetRoot([]byte)
		Key() []byte
		Dirty() bool
		Get(key []byte) (TreeNode, error)
		GetProof(key []byte, version Version) ([][]byte, []int, error)
		VerifyProof(proofs [][]byte, helpers []int, version Version) bool
		Set(key, val []byte, version Version) (TreeNode, error)
		Versions() []Version
		SetVersions([]Version)
		Depth() uint64
		Left() TreeNode
		Right() TreeNode
	}
)
