/*
Package coordinator handles all the logic related to forging batches as a
coordinator in the hermez network.

The forging of batches is done with a pipeline in order to allow multiple
batches being forged in parallel.  The maximum number of batches that can be
forged in parallel is determined by the number of available proof servers.

The Coordinator begins with the pipeline stopped.  The main Coordinator
goroutine keeps listening for synchronizer events sent by the node package,
which allow the coordinator to determine if the configured forger address is
allowed to forge at the current block or not.  When the forger address becomes
allowed forging, the pipeline is started, and when it terminates being allowed
to forge, the pipeline is stopped.

The Pipeline consists of two goroutines.  The first one is in charge of
preparing a batch internally, which involves making a selection of transactions
and calculating the ZKInputs for the batch proof, and sending these ZKInputs to
an idle proof server.  This goroutine will keep preparing batches while there
are idle proof servers, if the forging policy determines that a batch should be
forged in the current state.  The second goroutine is in charge of waiting for
the proof server to finish computing the proof, retrieving it, prepare the
arguments for the `forgeBatch` Rollup transaction, and sending the result to
the TxManager.  All the batch information moves between functions and
goroutines via the BatchInfo struct.

Finally, the TxManager contains a single goroutine that makes forgeBatch
ethereum transactions for the batches sent by the Pipeline, and keeps them in a
list to check them periodically.  In the periodic checks, the ethereum
transaction is checked for successfulness, and it's only forgotten after a
number of confirmation blocks have passed after being successfully mined.  At
any point if a transaction failure is detected, the TxManager can signal the
Coordinator to reset the Pipeline in order to reforge the failed batches.

The Coordinator goroutine acts as a manager.  The synchronizer events (which
notify about new blocks and associated new state) that it receives are
broadcasted to the Pipeline and the TxManager.  This allows the Coordinator,
Pipeline and TxManager to have a copy of the current hermez network state
required to perform their duties.
*/
package coordinator

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
	"tokamak-sybil-resistance/batchbuilder"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/config"
	"tokamak-sybil-resistance/coordinator/prover"
	"tokamak-sybil-resistance/database/historydb"
	"tokamak-sybil-resistance/eth"
	"tokamak-sybil-resistance/etherscan"
	"tokamak-sybil-resistance/log"
	"tokamak-sybil-resistance/synchronizer"
	"tokamak-sybil-resistance/txprocessor"
	"tokamak-sybil-resistance/txselector"

	ethCommon "github.com/ethereum/go-ethereum/common"
)

var errSkipBatchByPolicy = fmt.Errorf("skip batch by policy")

const (
	queueLen         = 16
	longWaitDuration = 999 * time.Hour
	zeroDuration     = 0 * time.Second
)

// Config contains the Coordinator configuration
type Config struct {
	// ForgerAddress is the address under which this coordinator is forging
	ForgerAddress ethCommon.Address
	// ConfirmBlocks is the number of confirmation blocks to wait for sent
	// ethereum transactions before forgetting about them
	ConfirmBlocks int64
	// L1BatchTimeoutPerc is the portion of the range before the L1Batch
	// timeout that will trigger a schedule to forge an L1Batch
	L1BatchTimeoutPerc float64
	// StartSlotBlocksDelay is the number of blocks of delay to wait before
	// starting the pipeline when we reach a slot in which we can forge.
	StartSlotBlocksDelay int64
	// ScheduleBatchBlocksAheadCheck is the number of blocks ahead in which
	// the forger address is checked to be allowed to forge (apart from
	// checking the next block), used to decide when to stop scheduling new
	// batches (by stopping the pipeline).
	// For example, if we are at block 10 and ScheduleBatchBlocksAheadCheck
	// is 5, even though at block 11 we canForge, the pipeline will be
	// stopped if we can't forge at block 15.
	// This value should be the expected number of blocks it takes between
	// scheduling a batch and having it mined.
	ScheduleBatchBlocksAheadCheck int64
	// SendBatchBlocksMarginCheck is the number of margin blocks ahead in
	// which the coordinator is also checked to be allowed to forge, apart
	// from the next block; used to decide when to stop sending batches to
	// the smart contract.
	// For example, if we are at block 10 and SendBatchBlocksMarginCheck is
	// 5, even though at block 11 we canForge, the batch will be discarded
	// if we can't forge at block 15.
	// This value should be the expected number of blocks it takes between
	// sending a batch and having it mined.
	SendBatchBlocksMarginCheck int64
	// EthClientAttempts is the number of attempts to do an eth client RPC
	// call before giving up
	EthClientAttempts int
	// ForgeRetryInterval is the waiting interval between calls forge a
	// batch after an error
	ForgeRetryInterval time.Duration
	// ForgeDelay is the delay after which a batch is forged if the slot is
	// already committed.  If set to 0s, the coordinator will continuously
	// forge at the maximum rate.
	ForgeDelay time.Duration
	// ForgeNoTxsDelay is the delay after which a batch is forged even if
	// there are no txs to forge if the slot is already committed.  If set
	// to 0s, the coordinator will continuously forge even if the batches
	// are empty.
	ForgeNoTxsDelay time.Duration
	// MustForgeAtSlotDeadline enables the coordinator to forge slots if
	// the empty slots reach the slot deadline.
	MustForgeAtSlotDeadline bool
	// IgnoreSlotCommitment disables forcing the coordinator to forge a
	// slot immediately when the slot is not committed. If set to false,
	// the coordinator will immediately forge a batch at the beginning of
	// a slot if it's the slot winner.
	IgnoreSlotCommitment bool
	// ForgeOncePerSlotIfTxs will make the coordinator forge at most one
	// batch per slot, only if there are included txs in that batch, or
	// pending l1UserTxs in the smart contract.  Setting this parameter
	// overrides `ForgeDelay`, `ForgeNoTxsDelay`, `MustForgeAtSlotDeadline`
	// and `IgnoreSlotCommitment`.
	ForgeOncePerSlotIfTxs bool
	// SyncRetryInterval is the waiting interval between calls to the main
	// handler of a synced block after an error
	SyncRetryInterval time.Duration
	// EthClientAttemptsDelay is delay between attempts do do an eth client
	// RPC call
	EthClientAttemptsDelay time.Duration
	// EthTxResendTimeout is the timeout after which a non-mined ethereum
	// transaction will be resent (reusing the nonce) with a newly
	// calculated gas price
	EthTxResendTimeout time.Duration
	// EthNoReuseNonce disables reusing nonces of pending transactions for
	// new replacement transactions
	EthNoReuseNonce bool
	// MaxGasPrice is the maximum gas price in gwei allowed for ethereum
	// transactions
	MaxGasPrice int64
	// MinGasPrice is the minimum gas price in gwei allowed for ethereum
	MinGasPrice int64
	// GasPriceIncPerc is the percentage increase of gas price set in an
	// ethereum transaction from the suggested gas price by the ehtereum
	// node
	GasPriceIncPerc int64
	// TxManagerCheckInterval is the waiting interval between receipt
	// checks of ethereum transactions in the TxManager
	TxManagerCheckInterval time.Duration
	// DebugBatchPath if set, specifies the path where batchInfo is stored
	// in JSON in every step/update of the pipeline
	DebugBatchPath string
	// VerifierIdx is the index of the verifier contract registered in the
	// smart contract
	// VerifierIdx uint8
	// ForgeBatchGasCost contains the cost of each action in the
	// ForgeBatch transaction.
	ForgeBatchGasCost config.ForgeBatchGasCost
	TxProcessorConfig txprocessor.Config
	ProverReadTimeout time.Duration
}

func (c *Config) debugBatchStore(batchInfo *BatchInfo) {
	if c.DebugBatchPath != "" {
		if err := batchInfo.DebugStore(c.DebugBatchPath); err != nil {
			log.Warnw("Error storing debug BatchInfo",
				"path", c.DebugBatchPath, "err", err)
		}
	}
}

type fromBatch struct {
	BatchNum   common.BatchNum
	ForgerAddr ethCommon.Address

	AccountRoot *big.Int
	VouchRoot   *big.Int
	ScoreRoot   *big.Int
}

// Coordinator implements the Coordinator type
type Coordinator struct {
	// State
	pipelineNum       int       // Pipeline sequential number.  The first pipeline is 1
	pipelineFromBatch fromBatch // batch from which we started the pipeline
	prover            prover.Client
	consts            common.SCConsts
	vars              common.SCVariables
	stats             synchronizer.Stats
	started           bool

	cfg Config

	historyDB    *historydb.HistoryDB
	txSelector   *txselector.TxSelector
	batchBuilder *batchbuilder.BatchBuilder

	msgCh  chan interface{}
	ctx    context.Context
	wg     sync.WaitGroup
	cancel context.CancelFunc

	pipeline              *Pipeline
	lastNonFailedBatchNum common.BatchNum

	txManager *TxManager
}

// MsgSyncBlock indicates an update to the Synchronizer stats
type MsgSyncBlock struct {
	Stats   synchronizer.Stats
	Batches []common.BatchData
	// Vars contains each Smart Contract variables if they are updated, or
	// nil if they haven't changed.
	Vars common.SCVariablesPtr
}

// MsgSyncReorg indicates a reorg
type MsgSyncReorg struct {
	Stats synchronizer.Stats
	Vars  common.SCVariablesPtr
}

// MsgStopPipeline indicates a signal to reset the pipeline
type MsgStopPipeline struct {
	Reason string
	// FailedBatchNum indicates the first batchNum that failed in the
	// pipeline.  If FailedBatchNum is 0, it should be ignored.
	FailedBatchNum common.BatchNum
}

// NewCoordinator creates a new Coordinator
func NewCoordinator(cfg Config,
	historyDB *historydb.HistoryDB,
	txSelector *txselector.TxSelector,
	batchBuilder *batchbuilder.BatchBuilder,
	prover prover.Client,
	ethClient eth.ClientInterface,
	scConsts *common.SCConsts,
	initSCVars *common.SCVariables,
	etherscanService *etherscan.Service,
) (*Coordinator, error) {
	if cfg.DebugBatchPath != "" {
		if err := os.MkdirAll(cfg.DebugBatchPath, 0744); err != nil {
			return nil, common.Wrap(err)
		}
	}


	ctx, cancel := context.WithCancel(context.Background())
	c := Coordinator{
		pipelineNum: 0,
		pipelineFromBatch: fromBatch{
			BatchNum:   0,
			ForgerAddr: ethCommon.Address{},

			AccountRoot: big.NewInt(0),
			VouchRoot:   big.NewInt(0),
			ScoreRoot:   big.NewInt(0),
		},
		prover: prover,
		consts: *scConsts,
		vars:   *initSCVars,

		cfg: cfg,

		historyDB:    historyDB,
		txSelector:   txSelector,
		batchBuilder: batchBuilder,

		msgCh: make(chan interface{}, queueLen),
		ctx:   ctx,
		// wg
		cancel: cancel,
	}
	ctxTimeout, ctxTimeoutCancel := context.WithTimeout(ctx, 1*time.Second)
	defer ctxTimeoutCancel()
	txManager, err := NewTxManager(
		ctxTimeout,
		&cfg,
		ethClient,
		&c,
		scConsts,
		initSCVars,
		etherscanService,
	)
	if err != nil {
		return nil, common.Wrap(err)
	}
	c.txManager = txManager
	// Set Eth LastBlockNum to -1 in stats so that stats.Synced() is
	// guaranteed to return false before it's updated with a real stats
	c.stats.Eth.LastBlock.Num = -1
	return &c, nil
}

// Start the coordinator
func (c *Coordinator) Start() {
	if c.started {
		log.Fatal("Coordinator already started")
	}
	c.started = true
	c.wg.Add(1)
	go func() {
		c.txManager.Run(c.ctx)
		c.wg.Done()
	}()

	c.wg.Add(1)
	go func() {
		timer := time.NewTimer(longWaitDuration)
		for {
			select {
			case <-c.ctx.Done():
				log.Info("Coordinator done")
				c.wg.Done()
				return
			case msg := <-c.msgCh:
				if err := c.handleMsg(c.ctx, msg); c.ctx.Err() != nil {
					continue
				} else if err != nil {
					log.Errorw("Coordinator.handleMsg", "err", err)
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(c.cfg.SyncRetryInterval)
					continue
				}
			case <-timer.C:
				timer.Reset(longWaitDuration)
				if !c.stats.Synced() {
					continue
				}
				if err := c.syncStats(c.ctx, &c.stats); c.ctx.Err() != nil {
					continue
				} else if err != nil {
					log.Errorw("Coordinator.syncStats", "err", err)
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(c.cfg.SyncRetryInterval)
					continue
				}
			}
		}
	}()
}

const stopCtxTimeout = 200 * time.Millisecond

func (c *Coordinator) syncSCVars(vars common.SCVariablesPtr) {
	updateSCVars(&c.vars, vars)
}

func updateSCVars(vars *common.SCVariables, update common.SCVariablesPtr) {
	if update.Rollup != nil {
		vars.Rollup = *update.Rollup
	}
}

// handle some arbitrary block sync, not necessarily the latest block
func (c *Coordinator) handleMsgSyncBlock(ctx context.Context, msg *MsgSyncBlock) error {
	c.stats = msg.Stats
	c.syncSCVars(msg.Vars)
	c.txManager.SetSyncStatsVars(ctx, &msg.Stats, &msg.Vars)

	// if the synced block is not the latest block, don't do anything
	if !c.stats.Synced() {
		return nil
	}

	// else, start forging pipeline
	return c.syncStats(ctx, &c.stats)
}

// NewPipeline creates a new Pipeline
func NewPipeline(
	ctx context.Context,
	cfg Config,
	num int, // Pipeline sequential number
	historyDB *historydb.HistoryDB,
	txSelector *txselector.TxSelector,
	batchBuilder *batchbuilder.BatchBuilder,
	coord *Coordinator,
	txManager *TxManager,
	prover prover.Client,
	scConsts *common.SCConsts,
) (*Pipeline, error) {
	ctxTimeout, ctxTimeoutCancel := context.WithTimeout(ctx, cfg.ProverReadTimeout)
	defer ctxTimeoutCancel()
	if err := prover.WaitReady(ctxTimeout); err != nil {
		log.Errorw("prover.WaitReady", "err", err)
	}

	return &Pipeline{
		num:          num,
		cfg:          cfg,
		historyDB:    historyDB,
		txSelector:   txSelector,
		batchBuilder: batchBuilder,
		prover:       prover,
		coord:        coord,
		txManager:    txManager,
		consts:       *scConsts,
		statsVarsCh:  make(chan statsVars, queueLen),
	}, nil
}

func (c *Coordinator) newPipeline(ctx context.Context) (*Pipeline, error) {
	c.pipelineNum++
	return NewPipeline(
		ctx,
		c.cfg,
		c.pipelineNum,
		c.historyDB,
		c.txSelector,
		c.batchBuilder,
		c,
		c.txManager,
		c.prover,
		&c.consts,
	)
}

// TxSelector returns the inner TxSelector
func (c *Coordinator) TxSelector() *txselector.TxSelector {
	return c.txSelector
}

// BatchBuilder returns the inner BatchBuilder
func (c *Coordinator) BatchBuilder() *batchbuilder.BatchBuilder {
	return c.batchBuilder
}

func (c *Coordinator) syncStats(ctx context.Context, stats *synchronizer.Stats) error {
	nextBlock := c.stats.Eth.LastBlock.Num + 1

	if c.pipeline == nil {
		log.Infow(
			"Coordinator: forging state begin",
			"block",
			nextBlock,
			"batch",
			stats.Sync.LastBatch.BatchNum,
		)
		fromBatch := fromBatch{
			BatchNum:   stats.Sync.LastBatch.BatchNum,
			ForgerAddr: stats.Sync.LastBatch.ForgerAddr,

			AccountRoot: stats.Sync.LastBatch.AccountRoot,
			VouchRoot:   stats.Sync.LastBatch.VouchRoot,
			ScoreRoot:   stats.Sync.LastBatch.ScoreRoot,
		}
		if c.lastNonFailedBatchNum > fromBatch.BatchNum {
			fromBatch.BatchNum = c.lastNonFailedBatchNum
			fromBatch.ForgerAddr = c.cfg.ForgerAddress

			fromBatch.AccountRoot = big.NewInt(0)
			fromBatch.VouchRoot = big.NewInt(0)
			fromBatch.ScoreRoot = big.NewInt(0)
		}

		var err error
		if c.pipeline, err = c.newPipeline(ctx); err != nil {
			return common.Wrap(err)
		}
		c.pipelineFromBatch = fromBatch

		// Start the pipeline
		if err := c.pipeline.Start(fromBatch.BatchNum, stats, &c.vars); err != nil {
			c.pipeline = nil
			return common.Wrap(err)
		}

	}

	return nil
}

// SendMsg is a thread safe method to pass a message to the Coordinator
func (c *Coordinator) SendMsg(ctx context.Context, msg interface{}) {
	select {
	case c.msgCh <- msg:
	case <-ctx.Done():
	}
}

func (c *Coordinator) handleReorg(ctx context.Context, msg *MsgSyncReorg) error {
	c.stats = msg.Stats
	c.syncSCVars(msg.Vars)
	c.txManager.SetSyncStatsVars(ctx, &msg.Stats, &msg.Vars)
	if c.pipeline != nil {
		c.pipeline.SetSyncStatsVars(ctx, &msg.Stats, &msg.Vars)
	}

	// TODO: discuss if implementing other roots (vouch and score) is necessary
	// A pipeline keeps a record of the last batch info from the rollup contract
	if c.stats.Sync.LastBatch.AccountRoot == nil ||
		c.pipelineFromBatch.AccountRoot == nil ||
		c.stats.Sync.LastBatch.AccountRoot.Cmp(c.pipelineFromBatch.AccountRoot) != 0 {
		// There's been a reorg and the batch state root from which the
		// pipeline was started has changed (probably because it was in
		// a block that was discarded), and it was sent by a different
		// coordinator than us.  That batch may never be in the main
		// chain, so we stop the pipeline  (it will be started again
		// once the node is in sync).
		log.Infow("Coordinator.handleReorg StopPipeline sync.LastBatch.ForgerAddr != cfg.ForgerAddr "+
			"& sync.LastBatch.AccountRoot != pipelineFromBatch.AccountRoot",
			"sync.LastBatch.AccountRoot", c.stats.Sync.LastBatch.AccountRoot,
			"pipelineFromBatch.AccountRoot", c.pipelineFromBatch.AccountRoot)
		// c.txManager.DiscardPipeline(ctx, c.pipelineNum)
		if err := c.handleStopPipeline(ctx, "reorg", 0); err != nil {
			return common.Wrap(err)
		}
	}
	return nil
}

// Stop the forging pipeline
func (p *Pipeline) Stop(ctx context.Context) {
	if !p.started {
		log.Fatal("Pipeline already stopped")
	}
	p.started = false
	log.Info("Stopping Pipeline...")
	p.cancel()
	p.wg.Wait()
	if err := p.prover.Cancel(ctx); ctx.Err() != nil {
		log.Errorw("prover.Cancel", "err", ctx.Err())
	} else if err != nil {
		log.Errorw("prover.Cancel", "err", err)
	}
}

// handleStopPipeline handles stopping the pipeline.  If failedBatchNum is 0,
// the next pipeline will start from the last state of the synchronizer,
// otherwise, it will state from failedBatchNum-1.
func (c *Coordinator) handleStopPipeline(
	ctx context.Context,
	reason string,
	failedBatchNum common.BatchNum,
) error {
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	log.Infow(
		"Coordinator.handleStopPipeline called with reason: ", reason,
		" and failedBatchNum: ", failedBatchNum,
	)

	batchNum := c.stats.Sync.LastBatch.BatchNum
	if failedBatchNum != 0 {
		batchNum = failedBatchNum - 1
	}
	if c.pipeline != nil {
		c.pipeline.Stop(c.ctx)
		c.pipeline = nil
	}
	c.lastNonFailedBatchNum = batchNum
	return nil
}

func (c *Coordinator) handleMsg(ctx context.Context, msg interface{}) error {
	switch msg := msg.(type) {
	case MsgSyncBlock:
		if err := c.handleMsgSyncBlock(ctx, &msg); err != nil {
			return common.Wrap(fmt.Errorf("Coordinator.handleMsgSyncBlock error: %w", err))
		}
	case MsgSyncReorg:
		if err := c.handleReorg(ctx, &msg); err != nil {
			return common.Wrap(fmt.Errorf("Coordinator.handleReorg error: %w", err))
		}
	case MsgStopPipeline:
		log.Infow("Coordinator received MsgStopPipeline", "reason", msg.Reason)
		if err := c.handleStopPipeline(ctx, msg.Reason, msg.FailedBatchNum); err != nil {
			return common.Wrap(fmt.Errorf("Coordinator.handleStopPipeline: %w", err))
		}
	default:
		log.Fatalw("Coordinator Unexpected Coordinator msg of type %T: %+v", msg, msg)
	}
	return nil
}

// Stop the coordinator
func (c *Coordinator) Stop() {
	if !c.started {
		log.Fatal("Coordinator already stopped")
	}
	c.started = false
	log.Infow("Stopping Coordinator...")
	c.cancel()
	c.wg.Wait()
	if c.pipeline != nil {
		ctx, cancel := context.WithTimeout(context.Background(), stopCtxTimeout)
		defer cancel()
		c.pipeline.Stop(ctx)
		c.pipeline = nil
	}
}
