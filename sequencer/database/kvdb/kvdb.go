package kvdb

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/log"

	"github.com/iden3/go-merkletree/db"
	"github.com/iden3/go-merkletree/db/pebble"
)

const (
	// PathBatchNum defines the subpath of the Batch Checkpoint in the
	// subpath of the KVDB
	PathBatchNum = "BatchNum"
	// PathCurrent defines the subpath of the current Batch in the subpath
	// of the KVDB
	PathCurrent = "current"
	// PathLast defines the subpath of the last Batch in the subpath
	// of the StateDB
	PathLast = "last"
	// DefaultKeep is the default value for the Keep parameter
	DefaultKeep = 128
)

var (
	// KeyCurrentBatch is used as key in the db to store the current BatchNum
	KeyCurrentBatch = []byte("k:currentbatch")
	// keyCurrentAccountIdx is used as key in the db to store the CurrentIdx
	keyCurrentAccountIdx = []byte("k:idxAccount")
	// keyCurrentVouchIdx is used as key in the db to store the CurrentIdx
	keyCurrentVouchIdx = []byte("k:idxVouch")
	// ErrNoLast is returned when the KVDB has been configured to not have
	// a Last checkpoint but a Last method is used
	ErrNoLast = fmt.Errorf("no last checkpoint")
)

// KVDB represents the Key-Value DB object
type KVDB struct {
	cfg Config
	db  *pebble.Storage
	// CurrentIdx holds the current Idx that the BatchBuilder is using
	CurrentAccountIdx common.AccountIdx
	CurrentVouchIdx   common.VouchIdx
	CurrentBatch      common.BatchNum
	mutexCheckpoint   sync.Mutex
	mutexDelOld       sync.Mutex
	wg                sync.WaitGroup
	last              *Last
}

// Last is a consistent view to the last batch of the stateDB that can
// be queried concurrently.
type Last struct {
	db   *pebble.Storage
	path string
	rw   sync.RWMutex
}

// Config of the KVDB
type Config struct {
	// Path where the checkpoints will be stored
	Path string
	// Keep is the number of old checkpoints to keep.  If 0, all
	// checkpoints are kept.
	Keep int
	// At every checkpoint, check that there are no gaps between the
	// checkpoints
	NoGapsCheck bool
	// NoLast skips having an opened DB with a checkpoint to the last
	// batchNum for thread-safe reads.
	NoLast bool
}

func (k *Last) setNew() error {
	k.rw.Lock()
	defer k.rw.Unlock()
	if k.db != nil {
		k.db.Close()
		k.db = nil
	}
	lastPath := path.Join(k.path, PathLast)
	if err := os.RemoveAll(lastPath); err != nil {
		return common.Wrap(err)
	}
	db, err := pebble.NewPebbleStorage(lastPath, false)
	if err != nil {
		return common.Wrap(err)
	}
	k.db = db
	return nil
}

func (k *Last) set(kvdb *KVDB, batchNum common.BatchNum) error {
	k.rw.Lock()
	defer k.rw.Unlock()
	if k.db != nil {
		k.db.Close()
		k.db = nil
	}
	lastPath := path.Join(k.path, PathLast)
	if err := kvdb.MakeCheckpointFromTo(batchNum, lastPath); err != nil {
		return common.Wrap(err)
	}
	db, err := pebble.NewPebbleStorage(lastPath, false)
	if err != nil {
		return common.Wrap(err)
	}
	k.db = db
	return nil
}

func (k *Last) close() {
	k.rw.Lock()
	defer k.rw.Unlock()
	if k.db != nil {
		k.db.Close()
		k.db = nil
	}
}

// NewKVDB creates a new KVDB, allowing to use an in-memory or in-disk storage.
// Checkpoints older than the value defined by `keep` will be deleted.
func NewKVDB(cfg Config) (*KVDB, error) {
	var sto *pebble.Storage
	var err error
	sto, err = pebble.NewPebbleStorage(path.Join(cfg.Path, PathCurrent), false)
	if err != nil {
		return nil, common.Wrap(err)
	}
	var last *Last
	if !cfg.NoLast {
		last = &Last{
			path: cfg.Path,
		}
	}
	kvdb := &KVDB{
		cfg:  cfg,
		db:   sto,
		last: last,
	}
	// load currentBatch
	kvdb.CurrentBatch, err = kvdb.GetCurrentBatch()
	if err != nil {
		return nil, common.Wrap(err)
	}

	// make reset (get checkpoint) at currentBatch
	err = kvdb.reset(kvdb.CurrentBatch, true)
	if err != nil {
		return nil, common.Wrap(err)
	}

	return kvdb, nil
}

func (k *KVDB) LastRead(fn func(db *pebble.Storage) error) error {
	if k.last == nil {
		return common.Wrap(ErrNoLast)
	}
	k.last.rw.RLock()
	defer k.last.rw.RUnlock()
	return fn(k.last.db)
}

// DB returns the *pebble.Storage from the KVDB
func (k *KVDB) DB() *pebble.Storage {
	return k.db
}

// StorageWithPrefix returns the db.Storage with the given prefix from the
// current KVDB
func (k *KVDB) StorageWithPrefix(prefix []byte) db.Storage {
	return k.db.WithPrefix(prefix)
}

// ResetFromSynchronizer performs a reset in the KVDB getting the state from
// synchronizerKVDB for the given batchNum.
func (k *KVDB) ResetFromSynchronizer(batchNum common.BatchNum, synchronizerKVDB *KVDB) error {
	if synchronizerKVDB == nil {
		return common.Wrap(fmt.Errorf("synchronizerKVDB can not be nil"))
	}

	currentPath := path.Join(k.cfg.Path, PathCurrent)
	if k.db != nil {
		k.db.Close()
		k.db = nil
	}

	// remove 'current'
	if err := os.RemoveAll(currentPath); err != nil {
		return common.Wrap(err)
	}
	// remove all checkpoints
	list, err := k.ListCheckpoints()
	if err != nil {
		return common.Wrap(err)
	}
	for _, bn := range list {
		if err := k.DeleteCheckpoint(common.BatchNum(bn)); err != nil {
			return common.Wrap(err)
		}
	}

	if batchNum == 0 {
		// if batchNum == 0, open the new fresh 'current'
		sto, err := pebble.NewPebbleStorage(currentPath, false)
		if err != nil {
			return common.Wrap(err)
		}
		k.db = sto
		k.CurrentAccountIdx = common.RollupConstReservedIDx // 255
		k.CurrentBatch = 0

		return nil
	}

	checkpointPath := path.Join(k.cfg.Path, fmt.Sprintf("%s%d", PathBatchNum, batchNum))

	// copy synchronizer 'BatchNumX' to 'BatchNumX'
	if err := synchronizerKVDB.MakeCheckpointFromTo(batchNum, checkpointPath); err != nil {
		return common.Wrap(err)
	}

	// copy 'BatchNumX' to 'current'
	err = k.MakeCheckpointFromTo(batchNum, currentPath)
	if err != nil {
		return common.Wrap(err)
	}

	// open the new 'current'
	sto, err := pebble.NewPebbleStorage(currentPath, false)
	if err != nil {
		return common.Wrap(err)
	}
	k.db = sto

	// get currentBatch num
	k.CurrentBatch, err = k.GetCurrentBatch()
	if err != nil {
		return common.Wrap(err)
	}
	// get currentIdx
	k.CurrentAccountIdx, err = k.GetCurrentAccountIdx()
	if err != nil {
		return common.Wrap(err)
	}

	return nil
}

// Reset resets the KVDB to the checkpoint at the given batchNum. Reset does
// not delete the checkpoints between old current and the new current, those
// checkpoints will remain in the storage, and eventually will be deleted when
// MakeCheckpoint overwrites them.
func (k *KVDB) Reset(batchNum common.BatchNum) error {
	return k.reset(batchNum, true)
}

// reset resets the KVDB to the checkpoint at the given batchNum. Reset does
// not delete the checkpoints between old current and the new current, those
// checkpoints will remain in the storage, and eventually will be deleted when
// MakeCheckpoint overwrites them.  `closeCurrent` will close the currently
// opened db before doing the reset.
func (k *KVDB) reset(batchNum common.BatchNum, closeCurrent bool) error {
	currentPath := path.Join(k.cfg.Path, PathCurrent)

	if closeCurrent && k.db != nil {
		k.db.Close()
		k.db = nil
	}
	// remove 'current'
	if err := os.RemoveAll(currentPath); err != nil {
		return common.Wrap(err)
	}
	// remove all checkpoints > batchNum
	list, err := k.ListCheckpoints()
	if err != nil {
		return common.Wrap(err)
	}
	// Find first batch that is greater than batchNum, and delete
	// everything after that
	start := 0
	for ; start < len(list); start++ {
		if common.BatchNum(list[start]) > batchNum {
			break
		}
	}
	for _, bn := range list[start:] {
		if err := k.DeleteCheckpoint(common.BatchNum(bn)); err != nil {
			return common.Wrap(err)
		}
	}

	if batchNum == 0 {
		// if batchNum == 0, open the new fresh 'current'
		sto, err := pebble.NewPebbleStorage(currentPath, false)
		if err != nil {
			return common.Wrap(err)
		}
		k.db = sto
		k.CurrentAccountIdx = common.RollupConstReservedIDx // 255
		//TODO: Need to check and update this for VouchIDx, For reseting the same
		k.CurrentVouchIdx = common.RollupConstReservedIDx
		k.CurrentBatch = 0
		if k.last != nil {
			if err := k.last.setNew(); err != nil {
				return common.Wrap(err)
			}
		}

		return nil
	}

	// copy 'batchNum' to 'current'
	if err := k.MakeCheckpointFromTo(batchNum, currentPath); err != nil {
		return common.Wrap(err)
	}
	// copy 'batchNum' to 'last'
	if k.last != nil {
		if err := k.last.set(k, batchNum); err != nil {
			return common.Wrap(err)
		}
	}

	// open the new 'current'
	sto, err := pebble.NewPebbleStorage(currentPath, false)
	if err != nil {
		return common.Wrap(err)
	}
	k.db = sto

	// get currentBatch num
	k.CurrentBatch, err = k.GetCurrentBatch()
	if err != nil {
		return common.Wrap(err)
	}
	// idx is obtained from the statedb reset
	k.CurrentAccountIdx, err = k.GetCurrentAccountIdx()
	if err != nil {
		return common.Wrap(err)
	}
	// idx is obtained from the statedb reset
	k.CurrentVouchIdx, err = k.GetCurrentVouchIdx()
	if err != nil {
		return common.Wrap(err)
	}

	return nil
}

// GetCurrentAccountIdx returns the stored Idx from the KVDB, which is the last Idx
// used for an Account in the k.
func (k *KVDB) GetCurrentAccountIdx() (common.AccountIdx, error) {
	idxBytes, err := k.db.Get(keyCurrentAccountIdx)
	if common.Unwrap(err) == db.ErrNotFound {
		return common.RollupConstReservedIDx, nil // 255, nil
	}
	if err != nil {
		return 0, common.Wrap(err)
	}
	return common.AccountIdxFromBytes(idxBytes[:])
}

// GetCurrentVouchIdx returns the stored Idx from the KVDB, which is the last Idx
// used for an Vouch in the k.
func (k *KVDB) GetCurrentVouchIdx() (common.VouchIdx, error) {
	idxBytes, err := k.db.Get(keyCurrentVouchIdx)
	if common.Unwrap(err) == db.ErrNotFound {
		//TODO: Need to check and update this for VouchIDx
		return common.RollupConstReservedIDx, nil // 255, nil
	}
	if err != nil {
		return 0, common.Wrap(err)
	}
	return common.VouchIdxFromBytes(idxBytes[:])
}

// GetCurrentBatch returns the current BatchNum stored in the KVDB
func (k *KVDB) GetCurrentBatch() (common.BatchNum, error) {
	cbBytes, err := k.db.Get(KeyCurrentBatch)
	if common.Unwrap(err) == db.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, common.Wrap(err)
	}
	return common.BatchNumFromBytes(cbBytes)
}

// setCurrentBatch stores the current BatchNum in the KVDB
func (k *KVDB) setCurrentBatch() error {
	tx, err := k.db.NewTx()
	if err != nil {
		return common.Wrap(err)
	}
	err = tx.Put(KeyCurrentBatch, k.CurrentBatch.Bytes())
	if err != nil {
		return common.Wrap(err)
	}
	if err := tx.Commit(); err != nil {
		return common.Wrap(err)
	}
	return nil
}

// SetCurrentIdx stores Idx in the KVDB - Account
func (k *KVDB) SetCurrentAccountIdx(idx common.AccountIdx) error {
	k.CurrentAccountIdx = idx

	tx, err := k.db.NewTx()
	if err != nil {
		return common.Wrap(err)
	}
	idxBytes, err := idx.Bytes()
	if err != nil {
		return common.Wrap(err)
	}
	err = tx.Put(keyCurrentAccountIdx, idxBytes[:])
	if err != nil {
		return common.Wrap(err)
	}
	if err := tx.Commit(); err != nil {
		return common.Wrap(err)
	}
	return nil
}

// SetCurrentIdx stores Idx in the KVDB - Vouch
func (k *KVDB) SetCurrentVouchIdx(idx common.VouchIdx) error {
	k.CurrentVouchIdx = idx

	tx, err := k.db.NewTx()
	if err != nil {
		return common.Wrap(err)
	}
	idxBytes, err := idx.Bytes()
	if err != nil {
		return common.Wrap(err)
	}
	err = tx.Put(keyCurrentVouchIdx, idxBytes[:])
	if err != nil {
		return common.Wrap(err)
	}
	if err := tx.Commit(); err != nil {
		return common.Wrap(err)
	}
	return nil
}

// ListCheckpoints returns the list of batchNums of the checkpoints, sorted.
// If there's a gap between the list of checkpoints, an error is returned.
func (k *KVDB) ListCheckpoints() ([]int, error) {
	files, err := os.ReadDir(k.cfg.Path)
	if err != nil {
		return nil, common.Wrap(err)
	}
	checkpoints := []int{}
	var checkpoint int
	pattern := fmt.Sprintf("%s%%d", PathBatchNum)
	for _, file := range files {
		fileName := file.Name()
		if file.IsDir() && strings.HasPrefix(fileName, PathBatchNum) {
			if _, err := fmt.Sscanf(fileName, pattern, &checkpoint); err != nil {
				return nil, common.Wrap(err)
			}
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	sort.Ints(checkpoints)
	if !k.cfg.NoGapsCheck && len(checkpoints) > 0 {
		first := checkpoints[0]
		for _, checkpoint := range checkpoints[1:] {
			first++
			if checkpoint != first {
				log.Fatalf("gap between checkpoints", "checkpoints", checkpoints)
				return nil, common.Wrap(fmt.Errorf("checkpoint gap at %v", checkpoint))
			}
		}
	}
	return checkpoints, nil
}

// DeleteCheckpoint removes if exist the checkpoint of the given batchNum
func (k *KVDB) DeleteCheckpoint(batchNum common.BatchNum) error {
	checkpointPath := path.Join(k.cfg.Path, fmt.Sprintf("%s%d", PathBatchNum, batchNum))

	if _, err := os.Stat(checkpointPath); os.IsNotExist(err) {
		return common.Wrap(fmt.Errorf("Checkpoint with batchNum %d does not exist in DB", batchNum))
	} else if err != nil {
		return common.Wrap(err)
	}

	return os.RemoveAll(checkpointPath)
}

// MakeCheckpointFromTo makes a checkpoint from the current db at fromBatchNum
// to the dest folder.  This method is locking, so it can be called from
// multiple places at the same time.
func (k *KVDB) MakeCheckpointFromTo(fromBatchNum common.BatchNum, dest string) error {
	source := path.Join(k.cfg.Path, fmt.Sprintf("%s%d", PathBatchNum, fromBatchNum))
	if _, err := os.Stat(source); os.IsNotExist(err) {
		// if kvdb does not have checkpoint at batchNum, return err
		return common.Wrap(fmt.Errorf("Checkpoint \"%v\" does not exist", source))
	} else if err != nil {
		return common.Wrap(err)
	}
	// By locking we allow calling MakeCheckpointFromTo from multiple
	// places at the same time for the same stateDB.  This allows the
	// synchronizer to do a reset to a batchNum at the same time as the
	// pipeline is doing a txSelector.Reset and batchBuilder.Reset from
	// synchronizer to the same batchNum
	k.mutexCheckpoint.Lock()
	defer k.mutexCheckpoint.Unlock()
	return PebbleMakeCheckpoint(source, dest)
}

// PebbleMakeCheckpoint is a hepler function to make a pebble checkpoint from
// source to dest.
func PebbleMakeCheckpoint(source, dest string) error {
	// Remove dest folder (if it exists) before doing the checkpoint
	if _, err := os.Stat(dest); os.IsNotExist(err) {
	} else if err != nil {
		return common.Wrap(err)
	} else {
		if err := os.RemoveAll(dest); err != nil {
			return common.Wrap(err)
		}
	}

	sto, err := pebble.NewPebbleStorage(source, false)
	if err != nil {
		return common.Wrap(err)
	}
	defer sto.Close()

	// execute Checkpoint
	err = sto.Pebble().Checkpoint(dest)
	if err != nil {
		return common.Wrap(err)
	}

	return nil
}

// MakeCheckpoint does a checkpoint at the given batchNum in the defined path.
// Internally this advances & stores the current BatchNum, and then stores a
// Checkpoint of the current state of the k.
func (k *KVDB) MakeCheckpoint() error {
	// advance currentBatch
	k.CurrentBatch++

	checkpointPath := path.Join(k.cfg.Path, fmt.Sprintf("%s%d", PathBatchNum, k.CurrentBatch))

	if err := k.setCurrentBatch(); err != nil {
		return common.Wrap(err)
	}

	// if checkpoint BatchNum already exist in disk, delete it
	if _, err := os.Stat(checkpointPath); os.IsNotExist(err) {
	} else if err != nil {
		return common.Wrap(err)
	} else {
		if err := os.RemoveAll(checkpointPath); err != nil {
			return common.Wrap(err)
		}
	}
	// execute Checkpoint
	if err := k.db.Pebble().Checkpoint(checkpointPath); err != nil {
		return common.Wrap(err)
	}
	// copy 'CurrentBatch' to 'last'
	if k.last != nil {
		if err := k.last.set(k, k.CurrentBatch); err != nil {
			return common.Wrap(err)
		}
	}

	k.wg.Add(1)
	go func() {
		delErr := k.DeleteOldCheckpoints()
		if delErr != nil {
			log.Errorw("delete old checkpoints failed", "err", delErr)
		}
		k.wg.Done()
	}()

	return nil
}

// DeleteOldCheckpoints deletes old checkpoints when there are more than
// `s.keep` checkpoints
func (k *KVDB) DeleteOldCheckpoints() error {
	k.mutexDelOld.Lock()
	defer k.mutexDelOld.Unlock()

	list, err := k.ListCheckpoints()
	if err != nil {
		return common.Wrap(err)
	}
	if k.cfg.Keep > 0 && len(list) > k.cfg.Keep {
		for _, checkpoint := range list[:len(list)-k.cfg.Keep] {
			if err := k.DeleteCheckpoint(common.BatchNum(checkpoint)); err != nil {
				return common.Wrap(err)
			}
		}
	}
	return nil
}

// Close the DB
func (k *KVDB) Close() {
	if k.db != nil {
		k.db.Close()
		k.db = nil
	}
	if k.last != nil {
		k.last.close()
	}
	// wait for deletion of old checkpoints
	k.wg.Wait()
}

// CheckpointExists returns true if the checkpoint exists
func (k *KVDB) CheckpointExists(batchNum common.BatchNum) (bool, error) {
	source := path.Join(k.cfg.Path, fmt.Sprintf("%s%d", PathBatchNum, batchNum))
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, common.Wrap(err)
	}
	return true, nil
}
