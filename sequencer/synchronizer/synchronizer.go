package synchronizer

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/database/historydb"
	"tokamak-sybil-resistance/database/statedb"
	"tokamak-sybil-resistance/eth"
	"tokamak-sybil-resistance/log"
	"tokamak-sybil-resistance/metric"
	"tokamak-sybil-resistance/txprocessor"

	"github.com/ethereum/go-ethereum"
	ethCommon "github.com/ethereum/go-ethereum/common"
)

const (
	// errStrUnknownBlock is the string returned by geth when querying an
	// unknown block
	errStrUnknownBlock = "unknown block"
)

var (
	// ErrUnknownBlock is the error returned by the Synchronizer when a
	// block is queried by hash but the ethereum node doesn't find it due
	// to it being discarded from a reorg.
	ErrUnknownBlock = fmt.Errorf("unknown block")
)

// Stats of the synchronizer
type Stats struct {
	Eth struct {
		UpdateBlockNumDiffThreshold uint16
		UpdateFrequencyDivider      uint16
		FirstBlockNum               int64
		LastBlock                   common.Block
		LastBatchNum                int64
	}
	Sync struct {
		Updated   time.Time
		LastBlock common.Block
		LastBatch common.Batch
		// LastL1BatchBlock is the last ethereum block in which an
		// l1Batch was forged
		LastL1BatchBlock  int64
		LastForgeL1TxsNum int64
		Auction           struct {
			CurrentSlot common.Slot
			NextSlot    common.Slot
		}
	}
}

// Synced returns true if the Synchronizer is up to date with the last ethereum block
func (s *Stats) Synced() bool {
	return s.Eth.LastBlock.Num == s.Sync.LastBlock.Num
}

// StatsHolder stores stats and that allows reading and writing them
// concurrently
type StatsHolder struct {
	Stats
	rw sync.RWMutex
}

// NewStatsHolder creates a new StatsHolder
func NewStatsHolder(firstBlockNum int64, updateBlockNumDiffThreshold uint16, updateFrequencyDivider uint16) *StatsHolder {
	stats := Stats{}
	stats.Eth.UpdateBlockNumDiffThreshold = updateBlockNumDiffThreshold
	stats.Eth.UpdateFrequencyDivider = updateFrequencyDivider
	stats.Eth.FirstBlockNum = firstBlockNum
	stats.Sync.LastForgeL1TxsNum = -1
	return &StatsHolder{Stats: stats}
}

// UpdateSync updates the synchronizer stats
func (s *StatsHolder) UpdateSync(lastBlock *common.Block, lastBatch *common.Batch,
	lastL1BatchBlock *int64, lastForgeL1TxsNum *int64) {
	now := time.Now()
	s.rw.Lock()
	s.Sync.LastBlock = *lastBlock
	if lastBatch != nil {
		s.Sync.LastBatch = *lastBatch
	}
	if lastL1BatchBlock != nil {
		s.Sync.LastL1BatchBlock = *lastL1BatchBlock
		s.Sync.LastForgeL1TxsNum = *lastForgeL1TxsNum
	}
	s.Sync.Updated = now
	s.rw.Unlock()
}

// CopyStats returns a copy of the inner Stats
func (s *StatsHolder) CopyStats() *Stats {
	s.rw.RLock()
	sCopy := s.Stats

	if s.Sync.LastBatch.AccountRoot != nil {
		sCopy.Sync.LastBatch.AccountRoot =
			common.CopyBigInt(s.Sync.LastBatch.AccountRoot)
	}
	if s.Sync.LastBatch.VouchRoot != nil {
		sCopy.Sync.LastBatch.VouchRoot =
			common.CopyBigInt(s.Sync.LastBatch.VouchRoot)
	}
	if s.Sync.LastBatch.ScoreRoot != nil {
		sCopy.Sync.LastBatch.ScoreRoot =
			common.CopyBigInt(s.Sync.LastBatch.ScoreRoot)
	}
	s.rw.RUnlock()
	return &sCopy
}

// Config is the Synchronizer configuration
type Config struct {
	StatsUpdateBlockNumDiffThreshold uint16
	StatsUpdateFrequencyDivider      uint16
	ChainID                          uint64
}

// Synchronizer implements the Synchronizer type
type Synchronizer struct {
	EthClient        eth.ClientInterface
	consts           *common.SCConsts
	historyDB        *historydb.HistoryDB
	stateDB          *statedb.StateDB
	cfg              Config
	initVars         common.SCVariables
	startBlockNum    int64
	vars             common.SCVariables
	stats            *StatsHolder
	resetStateFailed bool
}

// NewSynchronizer creates a new Synchronizer
func NewSynchronizer(
	ethClient eth.ClientInterface,
	historyDB *historydb.HistoryDB,
	stateDB *statedb.StateDB,
	cfg Config,
) (*Synchronizer, error) {
	rollupConstants, err := ethClient.RollupConstants()
	if err != nil {
		return nil, common.Wrap(fmt.Errorf("NewSynchronizer ethClient.RollupConstants(): %w",
			err))
	}

	consts := common.SCConsts{
		Rollup: *rollupConstants,
	}

	initVars, startBlockNum, err := getInitialVariables(ethClient /*&consts*/)
	if err != nil {
		return nil, common.Wrap(err)
	}

	stats := NewStatsHolder(startBlockNum, cfg.StatsUpdateBlockNumDiffThreshold, cfg.StatsUpdateFrequencyDivider)
	s := &Synchronizer{
		EthClient:     ethClient,
		consts:        &consts,
		historyDB:     historyDB,
		stateDB:       stateDB,
		cfg:           cfg,
		initVars:      *initVars,
		startBlockNum: startBlockNum,
		stats:         stats,
	}
	return s, s.init()
}

// StateDB returns the inner StateDB
func (s *Synchronizer) StateDB() *statedb.StateDB {
	return s.stateDB
}

// UpdateEth updates the ethereum stats, only if the previous stats expired
func (s *StatsHolder) UpdateEth(ethClient eth.ClientInterface) error {
	lastBlock, err := ethClient.EthBlockByNumber(context.TODO(), -1)
	if err != nil {
		return common.Wrap(fmt.Errorf("EthBlockByNumber: %w", err))
	}
	lastBatchNum, err := ethClient.RollupLastForgedBatch()
	if err != nil {
		return common.Wrap(fmt.Errorf("RollupLastForgedBatch: %w", err))
	}
	s.rw.Lock()
	s.Eth.LastBlock = *lastBlock
	s.Eth.LastBatchNum = lastBatchNum
	s.rw.Unlock()
	return nil
}

func (s *StatsHolder) blocksPerc() float64 {
	syncLastBlockNum := s.Sync.LastBlock.Num
	if s.Sync.LastBlock.Num == 0 {
		syncLastBlockNum = s.Eth.FirstBlockNum - 1
	}
	return float64(syncLastBlockNum-(s.Eth.FirstBlockNum-1)) * 100.0 /
		float64(s.Eth.LastBlock.Num-(s.Eth.FirstBlockNum-1))
}

func (s *StatsHolder) batchesPerc(batchNum common.BatchNum) float64 {
	return float64(batchNum) * 100.0 /
		float64(s.Eth.LastBatchNum)
}

// Stats returns a copy of the Synchronizer Stats.  It is safe to call Stats()
// during a Sync call
func (s *Synchronizer) Stats() *Stats {
	return s.stats.CopyStats()
}

func (s *Synchronizer) resetIntermediateState() error {
	lastBlock, err := s.historyDB.GetLastBlock()
	if common.Unwrap(err) == sql.ErrNoRows {
		lastBlock = &common.Block{}
	} else if err != nil {
		return common.Wrap(fmt.Errorf("historyDB.GetLastBlock: %w", err))
	}
	if err := s.resetState(lastBlock); err != nil {
		s.resetStateFailed = true
		return common.Wrap(fmt.Errorf("resetState at block %v: %w", lastBlock.Num, err))
	}
	s.resetStateFailed = false
	return nil
}

// Sync attempts to synchronize an ethereum block starting from lastSavedBlock.
// If lastSavedBlock is nil, the lastSavedBlock value is obtained from de DB.
// If a block is synced, it will be returned and also stored in the DB.  If a
// reorg is detected, the number of discarded blocks will be returned and no
// synchronization will be made.
func (s *Synchronizer) Sync(ctx context.Context,
	lastSavedBlock *common.Block) (blockData *common.BlockData, discarded *int64, err error) {
	if s.resetStateFailed {
		if err := s.resetIntermediateState(); err != nil {
			return nil, nil, common.Wrap(err)
		}
	}

	var nextBlockNum int64 // next block number to sync
	if lastSavedBlock == nil {
		// Get lastSavedBlock from History DB
		lastSavedBlock, err = s.historyDB.GetLastBlock()
		if err != nil && common.Unwrap(err) != sql.ErrNoRows {
			return nil, nil, common.Wrap(err)
		}
		// If we don't have any stored block, we must do a full sync
		// starting from the startBlockNum
		if common.Unwrap(err) == sql.ErrNoRows || lastSavedBlock.Num == 0 {
			nextBlockNum = s.startBlockNum
			lastSavedBlock = nil
		}
	}
	if lastSavedBlock != nil {
		nextBlockNum = lastSavedBlock.Num + 1
		if lastSavedBlock.Num < s.startBlockNum {
			return nil, nil, common.Wrap(
				fmt.Errorf("lastSavedBlock (%v) < startBlockNum (%v)",
					lastSavedBlock.Num, s.startBlockNum))
		}
	}

	ethBlock, err := s.EthClient.EthBlockByNumber(ctx, nextBlockNum)
	if common.Unwrap(err) == ethereum.NotFound {
		return nil, nil, nil
	} else if err != nil {
		return nil, nil, common.Wrap(fmt.Errorf("EthBlockByNumber: %w", err))
	}
	log.Debugf("ethBlock: num: %v, parent: %v, hash: %v",
		ethBlock.Num, ethBlock.ParentHash.String(), ethBlock.Hash.String())

	// While having more blocks to sync than UpdateBlockNumDiffThreshold, UpdateEth will be called once in
	// UpdateFrequencyDivider blocks
	if nextBlockNum+int64(s.stats.Eth.UpdateBlockNumDiffThreshold) >= s.stats.Eth.LastBlock.Num ||
		nextBlockNum%int64(s.stats.Eth.UpdateFrequencyDivider) == 0 {
		if err := s.stats.UpdateEth(s.EthClient); err != nil {
			return nil, nil, common.Wrap(err)
		}
	}

	log.Debugw("Syncing...",
		"block", nextBlockNum,
		"ethLastBlock", s.stats.Eth.LastBlock,
	)

	// Check that the obtained ethBlock.ParentHash == prevEthBlock.Hash; if not, reorg!
	if lastSavedBlock != nil {
		if lastSavedBlock.Hash != ethBlock.ParentHash {
			// Reorg detected
			log.Debugw("Reorg Detected",
				"blockNum", ethBlock.Num,
				"block.parent(got)", ethBlock.ParentHash, "parent.hash(exp)", lastSavedBlock.Hash)
			lastDBBlockNum, err := s.reorg(lastSavedBlock)
			if err != nil {
				return nil, nil, common.Wrap(err)
			}
			discarded := lastSavedBlock.Num - lastDBBlockNum
			metric.Reorgs.Inc()
			return nil, &discarded, nil
		}
	}

	defer func() {
		// If there was an error during sync, reset to the last block
		// in the historyDB because the historyDB is written last in
		// the Sync method and is the source of consistency.  This
		// allows resetting the stateDB in the case a batch was
		// processed but the historyDB block was not committed due to an
		// error.
		if err != nil {
			if err2 := s.resetIntermediateState(); err2 != nil {
				log.Errorw("sync revert", "err", err2)
			}
		}
	}()

	// Get data from the rollup contract
	rollupData, err := s.rollupSync(ethBlock)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}

	// for i := range rollupData.Withdrawals {
	// 	withdrawal := &rollupData.Withdrawals[i]
	// 	if !withdrawal.InstantWithdraw {
	// 		wDelayerTransfers := wDelayerData.DepositsByTxHash[withdrawal.TxHash]
	// 		if len(wDelayerTransfers) == 0 {
	// 			return nil, nil, common.Wrap(fmt.Errorf("WDelayer deposit corresponding to " +
	// 				"non-instant rollup withdrawal not found"))
	// 		}
	// 		// Pop the first wDelayerTransfer to consume them in chronological order
	// 		wDelayerTransfer := wDelayerTransfers[0]
	// 		wDelayerData.DepositsByTxHash[withdrawal.TxHash] =
	// 			wDelayerData.DepositsByTxHash[withdrawal.TxHash][1:]

	// 		withdrawal.Owner = wDelayerTransfer.Owner
	// 		withdrawal.Token = wDelayerTransfer.Token
	// 	}
	// }

	// Group all the block data into the structs to save into HistoryDB
	blockData = &common.BlockData{
		Block:  *ethBlock,
		Rollup: *rollupData,
	}

	err = s.historyDB.AddBlockSCData(blockData)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}

	batchesLen := len(rollupData.Batches)
	if batchesLen == 0 {
		s.stats.UpdateSync(ethBlock, nil, nil, nil)
	} else {
		var lastL1BatchBlock *int64
		var lastForgeL1TxsNum *int64
		for _, batchData := range rollupData.Batches {
			if batchData.L1Batch {
				lastL1BatchBlock = &batchData.Batch.EthBlockNum
				lastForgeL1TxsNum = batchData.Batch.ForgeL1TxsNum
			}
		}
		s.stats.UpdateSync(ethBlock,
			&rollupData.Batches[batchesLen-1].Batch,
			lastL1BatchBlock, lastForgeL1TxsNum)
	}
	// hasBatch := false
	// if len(rollupData.Batches) > 0 {
	// 	hasBatch = true
	// }
	// if err = s.updateCurrentNextSlotIfSync(false, hasBatch); err != nil {
	// 	return nil, nil, common.Wrap(err)
	// }

	for _, batchData := range rollupData.Batches {
		metric.LastBatchNum.Set(float64(batchData.Batch.BatchNum))
		metric.EthLastBatchNum.Set(float64(s.stats.Eth.LastBatchNum))
		log.Debugw("Synced batch",
			"syncLastBatch", batchData.Batch.BatchNum,
			"syncBatchesPerc", s.stats.batchesPerc(batchData.Batch.BatchNum),
			"ethLastBatch", s.stats.Eth.LastBatchNum,
		)
	}
	metric.LastBlockNum.Set(float64(s.stats.Sync.LastBlock.Num))
	metric.EthLastBlockNum.Set(float64(s.stats.Eth.LastBlock.Num))

	log.Debugw("Synced block",
		"syncLastBlockNum", s.stats.Sync.LastBlock.Num,
		"syncBlocksPerc", s.stats.blocksPerc(),
		"ethLastBlockNum", s.stats.Eth.LastBlock.Num,
	)

	return blockData, nil, nil
}

// reorg manages a reorg, updating History and State DB as needed.  Keeps
// checking previous blocks from the HistoryDB against the blockchain until a
// block hash match is found.  All future blocks in the HistoryDB and
// corresponding batches in StateBD are discarded.  Returns the last valid
// blockNum from the HistoryDB.
func (s *Synchronizer) reorg(uncleBlock *common.Block) (int64, error) {
	blockNum := uncleBlock.Num

	var block *common.Block
	for blockNum >= s.startBlockNum {
		ethBlock, err := s.EthClient.EthBlockByNumber(context.Background(), blockNum)
		if err != nil {
			return 0, common.Wrap(fmt.Errorf("ethClient.EthBlockByNumber: %w", err))
		}

		block, err = s.historyDB.GetBlock(blockNum)
		if err != nil {
			return 0, common.Wrap(fmt.Errorf("historyDB.GetBlock: %w", err))
		}
		if block.Hash == ethBlock.Hash {
			log.Debugf("Found valid block: %v", blockNum)
			break
		}
		blockNum--
	}
	total := uncleBlock.Num - block.Num
	log.Debugw("Discarding blocks", "total", total, "from", uncleBlock.Num, "to", block.Num+1)

	// Set History DB and State DB to the correct state
	if err := s.historyDB.Reorg(block.Num); err != nil {
		return 0, common.Wrap(err)
	}

	if err := s.resetState(block); err != nil {
		s.resetStateFailed = true
		return 0, common.Wrap(err)
	}
	s.resetStateFailed = false

	return block.Num, nil
}

func getInitialVariables(ethClient eth.ClientInterface,

/*consts *common.SCConsts*/) (*common.SCVariables, int64, error) {
	rollupInit, rollupInitBlock, err := ethClient.RollupEventInit(77877) //TODO: Check this with hermuz code
	if err != nil {
		return nil, 0, common.Wrap(fmt.Errorf("RollupEventInit: %w", err))
	}
	rollupVars := rollupInit.RollupVariables()
	return &common.SCVariables{
		Rollup: *rollupVars,
	}, rollupInitBlock, nil
}

func (s *Synchronizer) init() error {
	// Update stats parameters so that they have valid values before the
	// first Sync call
	if err := s.stats.UpdateEth(s.EthClient); err != nil {
		return common.Wrap(err)
	}
	lastBlock := &common.Block{}
	lastSavedBlock, err := s.historyDB.GetLastBlock()
	// `s.historyDB.GetLastBlock()` will never return `sql.ErrNoRows`
	// because we always have the default block 0 in the DB
	if err != nil {
		return common.Wrap(err)
	}
	// If we only have the default block 0,
	// make sure that the stateDB is clean
	if lastSavedBlock.Num == 0 {
		if err := s.stateDB.Reset(0); err != nil {
			return common.Wrap(err)
		}
	} else {
		lastBlock = lastSavedBlock
	}

	if err := s.resetState(lastBlock); err != nil {
		s.resetStateFailed = true
		return common.Wrap(err)
	}
	s.resetStateFailed = false

	log.Info("Sync init block",
		"syncLastBlock", s.stats.Sync.LastBlock,
		"ethFirstBlockNum", s.stats.Eth.FirstBlockNum,
		"ethLastBlock", s.stats.Eth.LastBlock,
	)
	log.Info("Sync init batch",
		"syncLastBatch", s.stats.Sync.LastBatch.BatchNum,
		"ethLastBatch", s.stats.Eth.LastBatchNum,
	)
	return nil
}

func (s *Synchronizer) resetState(block *common.Block) error {
	rollup, err := s.historyDB.GetSCVars()
	// If SCVars are not in the HistoryDB, this is probably the first run
	// of the Synchronizer: store the initial vars taken from config
	if common.Unwrap(err) == sql.ErrNoRows {
		vars := s.initVars
		log.Info("Setting initial SCVars in HistoryDB")
		if err = s.historyDB.SetInitialSCVars(&vars.Rollup); err != nil {
			return common.Wrap(fmt.Errorf("historyDB.SetInitialSCVars: %w", err))
		}
		s.vars.Rollup = *vars.Rollup.Copy()
		// Add initial boot coordinator to HistoryDB
		if err := s.historyDB.AddCoordinators([]common.Coordinator{{
			Forger:      ethCommon.HexToAddress(os.Getenv("BootCoordinator")),
			URL:         os.Getenv("BootCoordinatorURL"),
			EthBlockNum: s.initVars.Rollup.EthBlockNum, //TODO: Check this with Eth Block
		}}); err != nil {
			return common.Wrap(err)
		}
	} else if err != nil {
		return common.Wrap(err)
	} else {
		s.vars.Rollup = *rollup
	}

	batch, err := s.historyDB.GetLastBatch()
	if err != nil && common.Unwrap(err) != sql.ErrNoRows {
		return common.Wrap(fmt.Errorf("historyDB.GetLastBatchNum: %w", err))
	}
	if common.Unwrap(err) == sql.ErrNoRows {
		batch = &common.Batch{}
	}

	err = s.stateDB.Reset(batch.BatchNum)
	if err != nil {
		return common.Wrap(fmt.Errorf("stateDB.Reset: %w", err))
	}

	lastL1BatchBlockNum, err := s.historyDB.GetLastL1BatchBlockNum()
	if err != nil && common.Unwrap(err) != sql.ErrNoRows {
		return common.Wrap(fmt.Errorf("historyDB.GetLastL1BatchBlockNum: %w", err))
	}
	if common.Unwrap(err) == sql.ErrNoRows {
		lastL1BatchBlockNum = 0
	}

	lastForgeL1TxsNum, err := s.historyDB.GetLastL1TxsNum()
	if err != nil && common.Unwrap(err) != sql.ErrNoRows {
		return common.Wrap(fmt.Errorf("historyDB.GetLastL1BatchBlockNum: %w", err))
	}
	if common.Unwrap(err) == sql.ErrNoRows || lastForgeL1TxsNum == nil {
		n := int64(-1)
		lastForgeL1TxsNum = &n
	}

	s.stats.UpdateSync(block, batch, &lastL1BatchBlockNum, lastForgeL1TxsNum)
	return nil
}

// TODO: Update consts variable above to SConsts
// RollupConstants returns the RollupConstants read from the smart contract
func (s *Synchronizer) RollupConstants() *common.RollupConstants {
	return &s.consts.Rollup
}

// TODO: Need to check and Update type initialised above in Synchronizer struct RollupVariables -> to SCVariables
// SCVars returns a copy of the Smart Contract Variables
func (s *Synchronizer) SCVars() *common.SCVariables {
	return &common.SCVariables{
		Rollup: *s.vars.Rollup.Copy(),
	}
}

// rollupSync retrieves all the Rollup Smart Contract Data that happened at
// ethBlock.blockNum with ethBlock.Hash.
func (s *Synchronizer) rollupSync(ethBlock *common.Block) (*common.RollupData, error) {
	blockNum := ethBlock.Num
	var rollupData = common.NewRollupData()
	// var forgeL1TxsNum int64

	// Get rollup events in the block, and make sure the block hash matches
	// the expected one.
	rollupEvents, err := s.EthClient.RollupEventsByBlock(blockNum, &ethBlock.Hash)
	if err != nil && err.Error() == errStrUnknownBlock {
		return nil, common.Wrap(ErrUnknownBlock)
	} else if err != nil {
		return nil, common.Wrap(fmt.Errorf("RollupEventsByBlock: %w", err))
	}
	// No events in this block
	if rollupEvents == nil {
		return &rollupData, nil
	}

	var nextForgeL1TxsNum int64 // forgeL1TxsNum for the next L1Batch
	nextForgeL1TxsNumPtr, err := s.historyDB.GetLastL1TxsNum()
	if err != nil {
		return nil, common.Wrap(err)
	}
	if nextForgeL1TxsNumPtr != nil {
		nextForgeL1TxsNum = *nextForgeL1TxsNumPtr + 1
	} else {
		nextForgeL1TxsNum = 0
	}

	// Get L1UserTX
	rollupData.L1UserTxs, err = getL1UserTx(rollupEvents.L1UserTx, blockNum)
	if err != nil {
		return nil, common.Wrap(err)
	}

	// Get ForgeBatch events to get the L1CoordinatorTxs
	for _, evtForgeBatch := range rollupEvents.ForgeBatch {
		batchData := common.NewBatchData()
		// Get the input for each Tx
		forgeBatchArgs, sender, err := s.EthClient.RollupForgeBatchArgs(evtForgeBatch.EthTxHash,
			evtForgeBatch.L1UserTxsLen)
		if err != nil {
			return nil, common.Wrap(fmt.Errorf("RollupForgeBatchArgs: %w", err))
		}
		ethTxHash := evtForgeBatch.EthTxHash
		gasUsed := evtForgeBatch.GasUsed
		gasPrice := evtForgeBatch.GasPrice
		batchNum := common.BatchNum(evtForgeBatch.BatchNum)
		var l1UserTxs []common.L1Tx
		// Check if this is a L1Batch to get L1 Tx from it
		if forgeBatchArgs.L1Batch {
			// Get L1UserTxs with toForgeL1TxsNum, which correspond
			// to the L1UserTxs that are forged in this batch, so
			// that stateDB can process them.

			// First try to find them in HistoryDB.
			l1UserTxs, err = s.historyDB.GetUnforgedL1UserTxs(nextForgeL1TxsNum)
			if err != nil {
				return nil, common.Wrap(err)
			}
			// Apart from the DB, try to find them in this block.
			// This could happen because in a block there could be
			// multiple batches with L1Batch = true (although it's
			// a very rare case).  If not found in the DB and the
			// block doesn't contain the l1UserTxs, it means that
			// the L1UserTxs queue with toForgeL1TxsNum was closed
			// empty, so we leave `l1UserTxs` as an empty slice.
			for _, l1UserTx := range rollupData.L1UserTxs {
				if *l1UserTx.ToForgeL1TxsNum == nextForgeL1TxsNum {
					l1UserTxs = append(l1UserTxs, l1UserTx)
				}
			}
		}

		// l1TxsAuth := make([]common.AccountCreationAuth,
		// 	0, len(forgeBatchArgs.L1CoordinatorTxsAuths))
		// batchData.L1CoordinatorTxs = make([]common.L1Tx, 0, len(forgeBatchArgs.L1CoordinatorTxs))
		// // Get L1 Coordinator Txs
		// for i := range forgeBatchArgs.L1CoordinatorTxs {
		// 	l1CoordinatorTx := forgeBatchArgs.L1CoordinatorTxs[i]
		// 	l1CoordinatorTx.Position = position
		// 	// l1CoordinatorTx.ToForgeL1TxsNum = &forgeL1TxsNum
		// 	l1CoordinatorTx.UserOrigin = false
		// 	l1CoordinatorTx.EthBlockNum = blockNum
		// 	l1CoordinatorTx.BatchNum = &batchNum
		// 	l1CoordinatorTx.EthTxHash = ethTxHash
		// 	l1Tx, err := common.NewL1Tx(&l1CoordinatorTx)
		// 	if err != nil {
		// 		return nil, common.Wrap(err)
		// 	}

		// 	batchData.L1CoordinatorTxs = append(batchData.L1CoordinatorTxs, *l1Tx)
		// 	position++

		// 	// Create a slice of account creation auth to be
		// 	// inserted later if not exists
		// 	if l1CoordinatorTx.FromEthAddr != common.RollupConstEthAddressInternalOnly {
		// 		l1CoordinatorTxAuth := forgeBatchArgs.L1CoordinatorTxsAuths[i]
		// 		l1TxsAuth = append(l1TxsAuth, common.AccountCreationAuth{
		// 			EthAddr:   l1CoordinatorTx.FromEthAddr,
		// 			BJJ:       l1CoordinatorTx.FromBJJ,
		// 			Signature: l1CoordinatorTxAuth,
		// 		})
		// 	}

		// 	// fmt.Println("DGB l1coordtx")
		// }

		// Insert the slice of account creation auth
		// only if the node run as a coordinator

		if int(forgeBatchArgs.VerifierIdx) >= len(s.consts.Rollup.Verifiers) {
			return nil, common.Wrap(fmt.Errorf("forgeBatchArgs.VerifierIdx (%v) >= "+
				" len(s.consts.Rollup.Verifiers) (%v)",
				forgeBatchArgs.VerifierIdx, len(s.consts.Rollup.Verifiers)))
		}
		tpc := txprocessor.Config{
			NLevels: uint32(s.consts.Rollup.Verifiers[forgeBatchArgs.VerifierIdx].NLevels),
			MaxTx:   uint32(s.consts.Rollup.Verifiers[forgeBatchArgs.VerifierIdx].MaxTx),
			ChainID: s.cfg.ChainID,
			// MaxFeeTx: common.RollupConstMaxFeeIdxCoordinator,
			MaxL1Tx: common.RollupConstMaxL1Tx,
		}
		tp := txprocessor.NewTxProcessor(s.stateDB, tpc)

		// ProcessTxs updates poolL2Txs adding: Nonce (and also TokenID, but we don't use it).
		processTxsOut, err := tp.ProcessTxs(l1UserTxs /*, poolL2Txs*/)
		if err != nil {
			return nil, common.Wrap(err)
		}
		if s.stateDB.CurrentBatch() != batchNum {
			return nil, common.Wrap(fmt.Errorf("stateDB.BatchNum (%v) != "+
				"evtForgeBatch.BatchNum = (%v)",
				s.stateDB.CurrentBatch(), batchNum))
		}
		// if s.stateDB.AccountTree.Root().BigInt().Cmp(forgeBatchArgs.NewStRoot) != 0 {
		// 	return nil, common.Wrap(fmt.Errorf("stateDB.MTRoot (%v) != "+
		// 		"forgeBatchArgs.NewStRoot (%v)",
		// 		s.stateDB.AccountTree.Root().BigInt(), forgeBatchArgs.NewStRoot))
		// }

		// Set the BatchNum in the forged L1UserTxs
		for i := range l1UserTxs {
			l1UserTxs[i].BatchNum = &batchNum
		}
		batchData.L1UserTxs = l1UserTxs

		// Set batchNum in exits
		for i := range processTxsOut.ExitInfos {
			exit := &processTxsOut.ExitInfos[i]
			exit.BatchNum = batchNum
		}
		batchData.ExitTree = processTxsOut.ExitInfos

		for i := range processTxsOut.CreatedAccounts {
			createdAccount := &processTxsOut.CreatedAccounts[i]
			createdAccount.Nonce = 0
			createdAccount.Balance = big.NewInt(0)
			createdAccount.BatchNum = batchNum
		}
		batchData.CreatedAccounts = processTxsOut.CreatedAccounts

		batchData.UpdatedAccounts = make([]common.AccountUpdate, 0,
			len(processTxsOut.UpdatedAccounts))
		for _, acc := range processTxsOut.UpdatedAccounts {
			batchData.UpdatedAccounts = append(batchData.UpdatedAccounts,
				common.AccountUpdate{
					EthBlockNum: blockNum,
					BatchNum:    batchNum,
					Idx:         acc.Idx,
					Nonce:       acc.Nonce,
					Balance:     acc.Balance,
				})
		}

		// slotNum := int64(0)
		// if ethBlock.Num >= s.consts.Auction.GenesisBlockNum {
		// 	slotNum = (ethBlock.Num - s.consts.Auction.GenesisBlockNum) /
		// 		int64(s.consts.Auction.BlocksPerSlot)
		// }

		// Get Batch information
		batch := common.Batch{
			BatchNum:    batchNum,
			EthTxHash:   ethTxHash,
			EthBlockNum: blockNum,
			ForgerAddr:  *sender,

			AccountRoot: forgeBatchArgs.NewAccountRoot,
			VouchRoot:   forgeBatchArgs.NewVouchRoot,
			ScoreRoot:   forgeBatchArgs.NewScoreRoot,
			NumAccounts: len(batchData.CreatedAccounts),
			LastIdx:     forgeBatchArgs.NewLastIdx,
			ExitRoot:    forgeBatchArgs.NewExitRoot,
			// SlotNum:            slotNum,
			GasUsed:  gasUsed,
			GasPrice: gasPrice,
		}
		nextForgeL1TxsNumCpy := nextForgeL1TxsNum
		if forgeBatchArgs.L1Batch {
			batch.ForgeL1TxsNum = &nextForgeL1TxsNumCpy
			batchData.L1Batch = true
			nextForgeL1TxsNum++
		}
		batchData.Batch = batch
		rollupData.Batches = append(rollupData.Batches, *batchData)
	}

	// // Get Registered Tokens
	// for _, evtAddToken := range rollupEvents.AddToken {
	// 	var token common.Token

	// 	token.TokenID = common.TokenID(evtAddToken.TokenID)
	// 	token.EthAddr = evtAddToken.TokenAddress
	// 	token.EthBlockNum = blockNum

	// 	if consts, err := s.EthClient.EthERC20Consts(evtAddToken.TokenAddress); err != nil {
	// 		log.Warnw("Error retrieving ERC20 token constants", "addr", evtAddToken.TokenAddress)
	// 		token.Name = "ERC20_ETH_ERROR"
	// 		token.Symbol = "ERROR"
	// 		token.Decimals = 1
	// 	} else {
	// 		token.Name = cutStringMax(consts.Name, 20)
	// 		token.Symbol = cutStringMax(consts.Symbol, 10)
	// 		token.Decimals = consts.Decimals
	// 	}

	// 	rollupData.AddedTokens = append(rollupData.AddedTokens, token)
	// }

	rollupData.UpdateBucketWithdraw = make([]common.BucketUpdate, 0, len(rollupEvents.UpdateBucketWithdraw))
	for _, evt := range rollupEvents.UpdateBucketWithdraw {
		rollupData.UpdateBucketWithdraw = append(rollupData.UpdateBucketWithdraw,
			common.BucketUpdate{
				EthBlockNum: blockNum,
				NumBucket:   evt.NumBucket,
				BlockStamp:  evt.BlockStamp,
				Withdrawals: evt.Withdrawals,
			})
	}

	rollupData.Withdrawals = make([]common.WithdrawInfo, 0, len(rollupEvents.Withdraw))
	for _, evt := range rollupEvents.Withdraw {
		rollupData.Withdrawals = append(rollupData.Withdrawals, common.WithdrawInfo{
			Idx:             common.AccountIdx(evt.Idx),
			NumExitRoot:     common.BatchNum(evt.NumExitRoot),
			InstantWithdraw: evt.InstantWithdraw,
			TxHash:          evt.TxHash,
		})
	}

	// for _, evt := range rollupEvents.UpdateTokenExchange {
	// 	if len(evt.AddressArray) != len(evt.ValueArray) {
	// 		return nil, common.Wrap(fmt.Errorf("in RollupEventUpdateTokenExchange "+
	// 			"len(AddressArray) != len(ValueArray) (%v != %v)",
	// 			len(evt.AddressArray), len(evt.ValueArray)))
	// 	}
	// 	for i := range evt.AddressArray {
	// 		rollupData.TokenExchanges = append(rollupData.TokenExchanges,
	// 			common.TokenExchange{
	// 				EthBlockNum: blockNum,
	// 				Address:     evt.AddressArray[i],
	// 				ValueUSD:    int64(evt.ValueArray[i]),
	// 			})
	// 	}
	// }

	varsUpdate := false

	for _, evt := range rollupEvents.UpdateForgeL1L2BatchTimeout {
		s.vars.Rollup.ForgeL1L2BatchTimeout = evt.NewForgeL1L2BatchTimeout
		varsUpdate = true
	}

	// for _, evt := range rollupEvents.UpdateFeeAddToken {
	// 	s.vars.Rollup.FeeAddToken = evt.NewFeeAddToken
	// 	varsUpdate = true
	// }

	// for _, evt := range rollupEvents.UpdateWithdrawalDelay {
	// 	s.vars.Rollup.WithdrawalDelay = evt.NewWithdrawalDelay
	// 	varsUpdate = true
	// }

	// NOTE: We skip the event rollupEvents.SafeMode because the
	// implementation RollupEventsByBlock already inserts a non-existing
	// RollupEventUpdateBucketsParameters into UpdateBucketsParameters with
	// all the bucket values at 0 and SafeMode = true
	for _, evt := range rollupEvents.UpdateBucketsParameters {
		s.vars.Rollup.Buckets = make([]common.BucketParams, 0, len(evt.ArrayBuckets))
		for _, bucket := range evt.ArrayBuckets {
			s.vars.Rollup.Buckets = append(s.vars.Rollup.Buckets, common.BucketParams{
				CeilUSD:         bucket.CeilUSD,
				BlockStamp:      bucket.BlockStamp,
				Withdrawals:     bucket.Withdrawals,
				RateBlocks:      bucket.RateBlocks,
				RateWithdrawals: bucket.RateWithdrawals,
				MaxWithdrawals:  bucket.MaxWithdrawals,
			})
		}
		s.vars.Rollup.SafeMode = evt.SafeMode
		varsUpdate = true
	}

	if varsUpdate {
		s.vars.Rollup.EthBlockNum = blockNum
		rollupData.Vars = s.vars.Rollup.Copy()
	}

	return &rollupData, nil
}

func getL1UserTx(eventsL1UserTx []eth.RollupEventL1UserTx, blockNum int64) ([]common.L1Tx, error) {
	l1Txs := make([]common.L1Tx, len(eventsL1UserTx))
	for i := range eventsL1UserTx {
		eventsL1UserTx[i].L1UserTx.EthBlockNum = blockNum
		// Check validity of L1UserTx
		l1Tx, err := common.NewL1Tx(&eventsL1UserTx[i].L1UserTx)
		if err != nil {
			return nil, common.Wrap(err)
		}
		l1Txs[i] = *l1Tx
	}
	return l1Txs, nil
}
