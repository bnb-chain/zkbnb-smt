// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package bsmt

import (
	"sync"
)

const (
	hashSize    = 32
	versionSize = 40
)

func NewTreeNode(depth uint8, path uint64, nilHashes *nilHashes, hasher *Hasher) *TreeNode {
	treeNode := &TreeNode{
		nilHash:      nilHashes.Get(depth),
		nilChildHash: nilHashes.Get(depth + 4),
		path:         path,
		depth:        depth,
		hasher:       hasher,
		internalMu:   make([]sync.RWMutex, 14),
		internalVer:  make([]Version, 14),
	}
	for i := 0; i < 2; i++ {
		treeNode.Internals[i] = nilHashes.Get(depth + 1)
	}
	for i := 2; i < 6; i++ {
		treeNode.Internals[i] = nilHashes.Get(depth + 2)
	}
	for i := 6; i < 14; i++ {
		treeNode.Internals[i] = nilHashes.Get(depth + 3)
	}

	return treeNode
}

type InternalNode []byte

type TreeNode struct {
	mu        sync.RWMutex
	Children  [16]*TreeNode
	Internals [14]InternalNode
	Versions  []*VersionInfo

	nilHash      []byte
	nilChildHash []byte
	path         uint64
	depth        uint8
	hasher       *Hasher
	temporary    bool
	internalMu   []sync.RWMutex
	internalVer  []Version
}

// Root Get latest hash of a node
func (node *TreeNode) Root() []byte {
	node.mu.RLock()
	defer node.mu.RUnlock()

	if len(node.Versions) == 0 {
		return node.nilHash
	}
	return node.Versions[len(node.Versions)-1].Hash
}

// Root Get latest hash of a node without a lock
func (node *TreeNode) root() []byte {
	if len(node.Versions) == 0 {
		return node.nilHash
	}
	return node.Versions[len(node.Versions)-1].Hash
}

func (node *TreeNode) Set(hash []byte, version Version) {
	node.mu.Lock()
	defer node.mu.Unlock()

	node.newVersion(&VersionInfo{
		Ver:  version,
		Hash: hash,
	})
}

func (node *TreeNode) newVersion(version *VersionInfo) {
	if len(node.Versions) > 0 && node.Versions[len(node.Versions)-1].Ver == version.Ver {
		// a new version already exists, overwrite it
		node.Versions[len(node.Versions)-1] = version
		return
	}
	node.Versions = append(node.Versions, version)
}

func (node *TreeNode) SetChildren(child *TreeNode, nibble int, version Version) {
	node.mu.Lock()
	defer node.mu.Unlock()

	node.Children[nibble] = child

	left, right := node.nilChildHash, node.nilChildHash
	switch nibble % 2 {
	case 0:
		if node.Children[nibble] != nil {
			left = node.Children[nibble].Root()
		}
		if node.Children[nibble^1] != nil {
			right = node.Children[nibble^1].Root()
		}
	case 1:
		if node.Children[nibble] != nil {
			right = node.Children[nibble].Root()
		}
		if node.Children[nibble^1] != nil {
			left = node.Children[nibble^1].Root()
		}
	}
	prefix := 6
	for i := 4; i >= 1; i >>= 1 {
		nibble = nibble / 2
		node.Internals[prefix+nibble] = node.hasher.Hash(left, right)
		switch nibble % 2 {
		case 0:
			left = node.Internals[prefix+nibble]
			right = node.Internals[prefix+nibble^1]
		case 1:
			right = node.Internals[prefix+nibble]
			left = node.Internals[prefix+nibble^1]
		}
		prefix = prefix - i
	}
	// update current root node
	node.newVersion(&VersionInfo{
		Ver:  version,
		Hash: node.hasher.Hash(node.Internals[0], node.Internals[1]),
	})
}

// Recompute all internal hashes
func (node *TreeNode) ComputeInternalHash() {
	node.mu.Lock()
	defer node.mu.Unlock()

	// leaf node
	for i := 0; i < 15; i += 2 {
		left, right := node.nilChildHash, node.nilChildHash
		if node.Children[i] != nil {
			left = node.Children[i].Root()
		}
		if node.Children[i+1] != nil {
			right = node.Children[i+1].Root()
		}
		node.Internals[6+i/2] = node.hasher.Hash(left, right)
	}
	// internal node
	for i := 13; i > 1; i -= 2 {
		node.Internals[i/2-1] = node.hasher.Hash(node.Internals[i-1], node.Internals[i])
	}
}

func (node *TreeNode) Copy() *TreeNode {
	node.mu.RLock()
	defer node.mu.RUnlock()

	return &TreeNode{
		Children:     node.Children,
		Internals:    node.Internals,
		Versions:     node.Versions,
		nilHash:      node.nilHash,
		nilChildHash: node.nilChildHash,
		path:         node.path,
		depth:        node.depth,
		hasher:       node.hasher,
		temporary:    node.temporary,
		internalMu:   node.internalMu,
		internalVer:  node.internalVer,
	}
}

func (node *TreeNode) mark(nibble int) {
	//node.mu.Lock()
	//defer node.mu.Unlock()
	for _, i := range leafInternalMap[nibble] {
		node.Internals[i] = nil
	}
}

func (node *TreeNode) Prune(oldestVersion Version) uint64 {
	node.mu.Lock()
	defer node.mu.Unlock()

	if len(node.Versions) <= 1 {
		return 0
	}
	i := 0
	for ; i < len(node.Versions)-1; i++ {
		if node.Versions[i].Ver >= oldestVersion {
			break
		}
	}

	originSize := len(node.Versions) * versionSize
	if i > 0 && node.Versions[i].Ver > oldestVersion {
		node.Versions = node.Versions[i-1:]
		return uint64(originSize - len(node.Versions)*versionSize)
	}

	node.Versions = node.Versions[i:]
	return uint64(originSize - len(node.Versions)*versionSize)
}

func (node *TreeNode) Rollback(targetVersion Version) (bool, uint64) {
	node.mu.Lock()
	defer node.mu.Unlock()

	if len(node.Versions) == 0 {
		return false, 0
	}
	var next bool
	originSize := len(node.Versions) * versionSize
	i := len(node.Versions) - 1
	for ; i >= 0; i-- {
		if node.Versions[i].Ver <= targetVersion {
			break
		}
		next = true
	}
	node.Versions = node.Versions[:i+1]
	return next, uint64(originSize - len(node.Versions)*versionSize)
}

// The node has not been updated for a long time,
// the subtree is emptied, and needs to be re-read from the database when it needs to be modified.
func (node *TreeNode) archive() {
	for i := 0; i < len(node.Internals); i++ {
		node.Internals[i] = nil
	}
	for i := 0; i < len(node.Children); i++ {
		node.Children[i] = nil
	}
	node.temporary = true
}

// PreviousVersion returns the previous version number in the current TreeNode
func (node *TreeNode) PreviousVersion() Version {
	node.mu.RLock()
	defer node.mu.RUnlock()

	if len(node.Versions) <= 1 {
		return 0
	}
	return node.Versions[len(node.Versions)-2].Ver
}

// size returns the current node size
func (node *TreeNode) Size() uint64 {
	if node.temporary {
		return uint64(len(node.Versions) * versionSize)
	}
	return uint64(len(node.Versions)*versionSize + hashSize*len(node.Internals))
}

// Release nodes that have not been updated for a long time from memory.
// slowing down memory usage in runtime.
func (node *TreeNode) Release(oldestVersion Version) uint64 {
	node.mu.Lock()
	defer node.mu.Unlock()

	size := node.Size()
	for i := 0; i < len(node.Children); i++ {
		if node.Children[i] != nil {
			length := len(node.Children[i].Versions)
			if length > 0 && node.Children[i].Versions[length-1].Ver < oldestVersion {
				// check for the latest version and release it if it is older than the pruned version
				node.Children[i].archive()
				size += node.Children[i].Size()
			} else {
				size += node.Children[i].Release(oldestVersion)
			}
		}
	}
	return size
}

// The nodes without child data.
// will be extended when it needs to be searched down.
func (node *TreeNode) IsTemporary() bool {
	return node.temporary
}

func (node *TreeNode) ToStorageTreeNode() *StorageTreeNode {
	node.mu.RLock()
	defer node.mu.RUnlock()

	var children [16]*StorageLeafNode
	for i := 0; i < 16; i++ {
		if node.Children[i] != nil {
			children[i] = &StorageLeafNode{node.Children[i].Versions}
		}
	}
	return &StorageTreeNode{
		Children:  children,
		Internals: node.Internals,
		Versions:  node.Versions,
		Path:      node.path,
	}
}

type VersionInfo struct {
	Ver  Version
	Hash []byte
}

type StorageLeafNode struct {
	Versions []*VersionInfo `rlp:"optional"`
}

type StorageTreeNode struct {
	Children  [16]*StorageLeafNode `rlp:"optional"`
	Internals [14]InternalNode     `rlp:"optional"`
	Versions  []*VersionInfo       `rlp:"optional"`
	Path      uint64               `rlp:"optional"`
}

func (node *StorageTreeNode) ToTreeNode(depth uint8, nilHashes *nilHashes, hasher *Hasher) *TreeNode {
	treeNode := &TreeNode{
		Internals:    node.Internals,
		Versions:     node.Versions,
		nilHash:      nilHashes.Get(depth),
		nilChildHash: nilHashes.Get(depth + 4),
		path:         node.Path,
		depth:        depth,
		hasher:       hasher,
		internalMu:   make([]sync.RWMutex, 14),
		internalVer:  make([]Version, 14),
	}
	for i := 0; i < 16; i++ {
		if node.Children[i] != nil && len(node.Children[i].Versions) > 0 {
			treeNode.Children[i] = &TreeNode{
				Versions:     node.Children[i].Versions,
				nilHash:      nilHashes.Get(depth + 4),
				nilChildHash: nilHashes.Get(depth + 8),
				hasher:       hasher,
				temporary:    true,
				//depth:        depth + 4,
				//path:         16*treeNode.path + uint64(i),
			}
		}
	}

	return treeNode
}

func (node *TreeNode) latestVersion() Version {
	if len(node.Versions) <= 0 {
		return 0
	}
	return node.Versions[len(node.Versions)-1].Ver
}

func (node *TreeNode) latestVersionWithLock() Version {
	node.mu.RLock()
	defer node.mu.RUnlock()
	if len(node.Versions) <= 0 {
		return 0
	}
	return node.Versions[len(node.Versions)-1].Ver
}

// recompute inner node
func (node *TreeNode) recompute(child *TreeNode, journals *journal, version Version) bool {
	nibble := int(child.path & 0xf)
	left, right := node.nilChildHash, node.nilChildHash
	// if sibling haven't finished yet,quit; sibling will be charge for computing
	switch nibble % 2 {
	case 0:
		left = child.root()
		if sibling, exist := journals.get(journalKey{child.depth, child.path ^ 1}); exist {
			if sibling.latestVersionWithLock() < version {
				return false
			}
			right = sibling.root()
		} else if node.Children[nibble^1] != nil {
			right = node.Children[nibble^1].Root()
		}
	case 1:
		right = child.root()
		if sibling, exist := journals.get(journalKey{child.depth, child.path ^ 1}); exist {
			if sibling.latestVersionWithLock() < version {
				return false
			}
			left = sibling.root()
		} else if node.Children[nibble^1] != nil {
			left = node.Children[nibble^1].Root()
		}
	}
	prefix := 6
	for i := 4; i >= 1; i >>= 1 {
		nibble = nibble / 2
		hash, setBefore := node.setInternal(prefix+nibble, left, right, version)
		if setBefore {
			return false
		}
		siblingNibble := prefix + nibble ^ 1
		siblingHash := node.getInternal(siblingNibble)
		if siblingHash == nil {
			return false
		}
		switch nibble % 2 {
		case 0:
			left = hash
			right = siblingHash
		case 1:
			right = hash
			left = siblingHash
		}
		prefix = prefix - i
	}
	// update current root
	node.newVersion(&VersionInfo{
		Ver:  version,
		Hash: node.hasher.Hash(node.Internals[0], node.Internals[1]),
	})
	return true
}

func (node *TreeNode) setInternal(idx int, left []byte, right []byte, version Version) ([]byte, bool) {
	node.internalMu[idx].Lock()
	defer node.internalMu[idx].Unlock()
	if node.Internals[idx] != nil {
		return node.Internals[idx], true
	}
	hash := node.hasher.Hash(left, right)
	node.Internals[idx] = hash
	node.internalVer[idx] = version
	return hash, false
}

func (node *TreeNode) getInternal(idx int) []byte {
	node.internalMu[idx].RLock()
	defer node.internalMu[idx].RUnlock()
	return node.Internals[idx]
}

func (node *TreeNode) getChild(nibble int) *TreeNode {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.Children[nibble]
}

var leafInternalMap = map[int][]int{
	0:  {0, 2, 6},
	1:  {0, 2, 6},
	2:  {0, 2, 7},
	3:  {0, 2, 7},
	4:  {0, 3, 8},
	5:  {0, 3, 8},
	6:  {0, 3, 9},
	7:  {0, 3, 9},
	8:  {1, 4, 10},
	9:  {1, 4, 10},
	10: {1, 4, 11},
	11: {1, 4, 11},
	12: {1, 5, 12},
	13: {1, 5, 12},
	14: {1, 5, 13},
	15: {1, 5, 13},
}
