package statedb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

<<<<<<< HEAD
<<<<<<< HEAD
	"github.com/cockroachdb/pebble"
=======
	"github.com/hermeznetwork/tracerr"
=======
>>>>>>> 5abb445 (Removed tracer imports from hermuz and used helpers)
	"github.com/iden3/go-merkletree"
	"github.com/iden3/go-merkletree/db/pebble"
>>>>>>> e0ccd8a (Updated util functionalities for database and apis)
)

<<<<<<< HEAD
// TreeNode represents a node in the Merkle tree.
type TreeNode struct {
	Hash  string
	Left  *TreeNode
	Right *TreeNode
=======
const (
	// TypeSynchronizer defines a StateDB used by the Synchronizer, that
	// generates the ExitTree when processing the txs
	TypeSynchronizer = "synchronizer"
	// TypeTxSelector defines a StateDB used by the TxSelector, without
	// computing ExitTree neither the ZKInputs
	TypeTxSelector = "txselector"
	// TypeBatchBuilder defines a StateDB used by the BatchBuilder, that
	// generates the ExitTree and the ZKInput when processing the txs
	TypeBatchBuilder = "batchbuilder"
	// MaxNLevels is the maximum value of NLevels for the merkle tree,
	// which comes from the fact that AccountIdx has 48 bits.
	MaxNLevels = 48
)

// Config of the StateDB
type Config struct {
	// Path where the checkpoints will be stored
	Path string
	// Keep is the number of old checkpoints to keep.  If 0, all
	// checkpoints are kept.
	Keep int
	// NoLast skips having an opened DB with a checkpoint to the last
	// batchNum for thread-safe reads.
	NoLast bool
	// Type of StateDB (
	Type TypeStateDB
	// NLevels is the number of merkle tree levels in case the Type uses a
	// merkle tree.  If the Type doesn't use a merkle tree, NLevels should
	// be 0.
	NLevels int
	// At every checkpoint, check that there are no gaps between the
	// checkpoints
	noGapsCheck bool
>>>>>>> 7615d4b (Initial implementation of  txProcessor)
}

<<<<<<< HEAD
// MerkleTree represents a Merkle tree.
type MerkleTree struct {
	Root *TreeNode
}

// Account represents an account with a link subtree.
type Account struct {
	Address  string
	Deposit  int
	Nonce    int
	Score    int
	LinkRoot string // Root hash of the Links Subtree
}

// Link represents a link structure.
type Link struct {
	TargetID string
	Stake    int
}

// hashData computes the SHA-256 hash of the input data.
func hashData(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
=======
var (
	// ErrStateDBWithoutMT is used when a method that requires a MerkleTree
	// is called in a StateDB that does not have a MerkleTree defined
	ErrStateDBWithoutMT = errors.New(
		"Can not call method to use MerkleTree in a StateDB without MerkleTree")
	// ErrIdxNotFound is used when trying to get the Idx from EthAddr or
	// EthAddr&ToBJJ
	ErrIdxNotFound = errors.New("Idx can not be found")
	// ErrGetIdxNoCase is used when trying to get the Idx from EthAddr &
	// BJJ with not compatible combination
	ErrGetIdxNoCase = errors.New(
		"Can not get Idx due unexpected combination of ethereum Address & BabyJubJub PublicKey")

	// PrefixKeyMT is the key prefix for merkle tree in the db
	PrefixKeyMT = []byte("m:")
)

// TypeStateDB determines the type of StateDB
type TypeStateDB string

// StateDB represents the state database with an integrated Merkle tree.
type StateDB struct {
	cfg         Config
	db          *kvdb.KVDB
	AccountTree *merkletree.MerkleTree
	VouchTree   *merkletree.MerkleTree
>>>>>>> d31cee2 (feat/go-synchronizer initial construction of stateDB)
}

// LocalStateDB represents the local StateDB which allows to make copies from
// the synchronizer StateDB, and is used by the tx-selector and the
// batch-builder. LocalStateDB is an in-memory storage.
type LocalStateDB struct {
	*StateDB
	synchronizerStateDB *StateDB
}

// initializeDB initializes and returns a Pebble DB instance.
func initializeDB(path string) (*pebble.DB, error) {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		return nil, err
	}
	return db, nil
}

// closeDB closes the Pebble DB instance.
func closeDB(db *pebble.DB) error {
	return db.Close()
}

// StateDB represents the state database with an integrated Merkle tree.
type StateDB struct {
	DB   *pebble.DB
	Tree *MerkleTree
}

// NewStateDB initializes a new StateDB.
func NewStateDB(dbPath string) (*StateDB, error) {
	db, err := initializeDB(dbPath)
	if err != nil {
		return nil, err
	}
<<<<<<< HEAD
	return &StateDB{
<<<<<<< HEAD
		DB:   db,
		Tree: &MerkleTree{},
=======
=======

	mtAccount, _ := merkletree.NewMerkleTree(kv.StorageWithPrefix(PrefixKeyMTAcc), 24)
	mtVouch, _ := merkletree.NewMerkleTree(kv.StorageWithPrefix(PrefixKeyMTVoc), 24)
	mtScore, _ := merkletree.NewMerkleTree(kv.StorageWithPrefix(PrefixKeyMTSco), 24)
	return &StateDB{
		cfg:         cfg,
>>>>>>> 7615d4b (Initial implementation of  txProcessor)
		db:          kv,
		AccountTree: mtAccount,
		VouchTree:   mtLink,
>>>>>>> d31cee2 (feat/go-synchronizer initial construction of stateDB)
	}, nil
}

// Type returns the StateDB configured Type
func (s *StateDB) Type() TypeStateDB {
	return s.cfg.Type
}

// Close closes the StateDB.
func (sdb *StateDB) Close() error {
	return closeDB(sdb.DB)
}

<<<<<<< HEAD
// Put stores an account in the database and updates the Merkle tree.
func (sdb *StateDB) PutAccount(account *Account) error {
	accountBytes, err := json.Marshal(account)
	if err != nil {
		return err
=======
// NewLocalStateDB returns a new LocalStateDB connected to the given
// synchronizerDB.  Checkpoints older than the value defined by `keep` will be
// deleted.
func NewLocalStateDB(cfg Config, synchronizerDB *StateDB) (*LocalStateDB, error) {
	cfg.noGapsCheck = true
	cfg.NoLast = true
	s, err := NewStateDB(cfg)
	if err != nil {
		return nil, common.Wrap(err)
	}
	return &LocalStateDB{
		s,
		synchronizerDB,
	}, nil
}

// Reset resets the StateDB to the checkpoint at the given batchNum. Reset
// does not delete the checkpoints between old current and the new current,
// those checkpoints will remain in the storage, and eventually will be
// deleted when MakeCheckpoint overwrites them.
func (s *StateDB) Reset(batchNum common.BatchNum) error {
<<<<<<< HEAD
	log.Fatalf("Making StateDB Reset", "batch", batchNum, "type", s.cfg.Type)
	if err := s.DB.Reset(batchNum); err != nil {
=======
	log.Debugw("Making StateDB Reset", "batch", batchNum, "type", s.cfg.Type)
	if err := s.db.Reset(batchNum); err != nil {
>>>>>>> 792abc7 (feat/go-synchronizer basic setup for synchronizer and sync for rollup genesis block)
		return common.Wrap(err)
>>>>>>> e0ccd8a (Updated util functionalities for database and apis)
	}

	err = sdb.DB.Set([]byte(account.Address), accountBytes, nil)
	if err != nil {
		return err
	}
<<<<<<< HEAD

	leaf := &TreeNode{Hash: hashData(string(accountBytes))}
	if sdb.Tree.Root == nil {
		sdb.Tree.Root = leaf
	} else {
		updateMerkleTree(sdb.Tree, leaf)
=======
	if s.VouchTree != nil {
		// open the MT for the current s.db
		vouchTree, err := merkletree.NewMerkleTree(s.db.StorageWithPrefix(PrefixKeyMT), s.VouchTree.MaxLevels())
		if err != nil {
			return common.Wrap(err)
		}
		s.VouchTree = vouchTree
>>>>>>> d31cee2 (feat/go-synchronizer initial construction of stateDB)
	}

	return nil
}

<<<<<<< HEAD
<<<<<<< HEAD
// Get retrieves an account for a given address from the database.
func (sdb *StateDB) GetAccount(address string) (*Account, error) {
	value, closer, err := sdb.DB.Get([]byte(address))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var account Account
	err = json.Unmarshal(value, &account)
	if err != nil {
		return nil, err
	}

	return &account, nil
}

// UpdateLink updates the link tree for an account and updates the account's link root in the database.
func (sdb *StateDB) UpdateLink(address string, link *Link) error {
	account, err := sdb.GetAccount(address)
	if err != nil {
		return err
	}

	// Deserialize the existing link tree if it exists
	var linkTree MerkleTree
	if account.LinkRoot != "" {
		linkTree.Root = &TreeNode{Hash: account.LinkRoot}
	}

	linkBytes, err := json.Marshal(link)
	if err != nil {
		return err
	}

	linkLeaf := &TreeNode{Hash: hashData(string(linkBytes))}
	updateMerkleTree(&linkTree, linkLeaf)

	account.LinkRoot = linkTree.Root.Hash

	return sdb.PutAccount(account)
}

// GetMerklePath retrieves the Merkle path for a given key-value pair.
func (sdb *StateDB) GetMerklePath(key, value string) ([]string, error) {
	targetHash := hashData(key + value)
	path, found := FindPathToRoot(sdb.Tree.Root, targetHash)
	if !found {
		return nil, fmt.Errorf("path not found for key: %s", key)
	}
	return path, nil
}

func performActionsAccount(a *Account, s *StateDB) {
	err := s.PutAccount(a)
	if err != nil {
		log.Fatalf("Failed to store key-value pair: %v", err)
	}

	// Retrieve and print a value
	value, err := s.GetAccount(a.Address)
	if err != nil {
		log.Fatalf("Failed to retrieve value: %v", err)
	}
	fmt.Printf("Retrieved account: %+v\n", value)

	// Print Merkle path
	path, _ := GetMerkelTreePath(s, a.Address)
	fmt.Printf("Merkle path: %v\n", path)

	// Get and print root hash for leaf
	GetRootHash(s, a.Address)

}

func printExamples(s *StateDB) {
	// Example accounts
	accountA := &Account{
		Address: "account_0xA",
		Deposit: 10,
		Nonce:   1,
		Score:   0,
	}

	accountB := &Account{
		Address: "account_0xB",
		Deposit: 5,
		Nonce:   2,
		Score:   0,
	}

	accountC := &Account{
		Address: "account_0xC",
		Deposit: 5,
		Nonce:   2,
		Score:   0,
	}

	accountD := &Account{
		Address: "account_0xD",
		Deposit: 5,
		Nonce:   2,
		Score:   0,
	}
	// Add Account A
	performActionsAccount(accountA, s)

	// Add Account B
	performActionsAccount(accountB, s)

	//Add Account C
	performActionsAccount(accountC, s)

	//Add Account D
	performActionsAccount(accountD, s)

	// Print Merkle tree root
	fmt.Printf("Merkle Tree Root: %s\n", s.Tree.Root.Hash)
}

func InitNewStateDB() *StateDB {
	// Initialize the StateDB
	stateDB, err := NewStateDB("stateDB")
	if err != nil {
		log.Fatalf("Failed to initialize StateDB: %v", err)
	}
	defer stateDB.Close()
	printExamples(stateDB)
	return stateDB
=======
=======
// MakeCheckpoint does a checkpoint at the given batchNum in the defined path.
// Internally this advances & stores the current BatchNum, and then stores a
// Checkpoint of the current state of the StateDB.
func (s *StateDB) MakeCheckpoint() error {
	log.Debugw("Making StateDB checkpoint", "batch", s.CurrentBatch()+1, "type", s.cfg.Type)
	return s.db.MakeCheckpoint()
}

>>>>>>> 7615d4b (Initial implementation of  txProcessor)
// CurrentBatch returns the current in-memory CurrentBatch of the StateDB.db
func (s *StateDB) CurrentBatch() common.BatchNum {
	return s.db.CurrentBatch
>>>>>>> 792abc7 (feat/go-synchronizer basic setup for synchronizer and sync for rollup genesis block)
}
