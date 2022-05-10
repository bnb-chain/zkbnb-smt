package bsmt

var (
	_ TreeNode = (*NilHashTreeNode)(nil)
	_ TreeNode = (*ShortTreeNode)(nil)
	_ TreeNode = (*FullTreeNode)(nil)
)

type NilHashTreeNode struct {
}

type FullTreeNode struct {
	LatestHash []byte
	Versions   []uint64

	// In-Memory
	Depth      uint8
	LeftChild  TreeNode
	RightChild TreeNode
	Dirty      bool
	Size       uint64
}

type ShortTreeNode struct {
	LatestHash []byte
	Versions   []uint64
}
