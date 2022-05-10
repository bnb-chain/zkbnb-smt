package bsmt

var (
	_ TreeNode = (*StorageValueNode)(nil)
	_ TreeNode = (*StorageShortTreeNode)(nil)
	_ TreeNode = (*StorageFullTreeNode)(nil)
)

type StorageFullTreeNode struct {
	LatestHash []byte
	Versions   []uint64
	Children   [30]StorageShortTreeNode
}

type StorageShortTreeNode struct {
	LatestHash []byte
	Versions   []uint64
}

type StorageValueNode []byte
