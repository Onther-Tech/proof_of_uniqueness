package coordinator

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
	"tokamak-sybil-resistance/batchbuilder"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/coordinator/prover"
	"tokamak-sybil-resistance/database/historydb"
	"tokamak-sybil-resistance/log"
	"tokamak-sybil-resistance/synchronizer"
	"tokamak-sybil-resistance/txselector"
)

type statsVars struct {
	Stats synchronizer.Stats
	Vars  common.SCVariablesPtr
}

type state struct {
	batchNum                     common.BatchNum
	lastScheduledL1BatchBlockNum int64
	lastForgeL1TxsNum            int64
}

// Pipeline manages the forging of batches with parallel server proofs
type Pipeline struct {
	num    int
	cfg    Config
	consts common.SCConsts

	// state
	state         state
	started       bool
	rw            sync.RWMutex
	errAtBatchNum common.BatchNum
	lastForgeTime time.Time

	prover       prover.Client
	coord        *Coordinator
	txManager    *TxManager
	historyDB    *historydb.HistoryDB
	txSelector   *txselector.TxSelector
	batchBuilder *batchbuilder.BatchBuilder

	stats       synchronizer.Stats
	vars        common.SCVariables
	statsVarsCh chan statsVars

	ctx    context.Context
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// reset pipeline state
func (p *Pipeline) reset(
	batchNum common.BatchNum,
	stats *synchronizer.Stats,
	vars *common.SCVariables,
) error {
	p.state = state{
		batchNum:                     batchNum,
		lastForgeL1TxsNum:            stats.Sync.LastForgeL1TxsNum,
		lastScheduledL1BatchBlockNum: 0,
	}
	p.stats = *stats
	p.vars = *vars

	// Reset the StateDB in TxSelector and BatchBuilder from the
	// synchronizer only if the checkpoint we reset from either:
	// a. Doesn't exist in the TxSelector/BatchBuilder
	// b. The batch has already been synced by the synchronizer and has a
	//    different MTRoot than the BatchBuilder
	// Otherwise, reset from the local checkpoint.

	// First attempt to reset from local checkpoint if such checkpoint exists
	existsTxSelector, err := p.txSelector.LocalAccountsDB().CheckpointExists(p.state.batchNum)
	if err != nil {
		return common.Wrap(err)
	}
	fromSynchronizerTxSelector := !existsTxSelector
	if err := p.txSelector.Reset(p.state.batchNum, fromSynchronizerTxSelector); err != nil {
		return common.Wrap(err)
	}
	existsBatchBuilder, err := p.batchBuilder.LocalStateDB().CheckpointExists(p.state.batchNum)
	if err != nil {
		return common.Wrap(err)
	}
	fromSynchronizerBatchBuilder := !existsBatchBuilder
	if err := p.batchBuilder.Reset(p.state.batchNum, fromSynchronizerBatchBuilder); err != nil {
		return common.Wrap(err)
	}

	// TODO: discuss if it's necessary to check all the roots or just one is enough
	// After reset, check that if the batch exists in the historyDB, the
	// stateRoot matches with the local one, if not, force a reset from
	// synchronizer
	batch, err := p.historyDB.GetBatch(p.state.batchNum)
	if common.Unwrap(err) == sql.ErrNoRows {
		// nothing to do
	} else if err != nil {
		return common.Wrap(err)
	} else {
		localStateAccountRoot := p.batchBuilder.LocalStateDB().AccountTree.Root().BigInt()
		localStateScoreRoot := p.batchBuilder.LocalStateDB().ScoreTree.Root().BigInt()
		localStateVouchRoot := p.batchBuilder.LocalStateDB().VouchTree.Root().BigInt()

		if batch.AccountRoot.Cmp(localStateAccountRoot) != 0 ||
			batch.ScoreRoot.Cmp(localStateScoreRoot) != 0 ||
			batch.VouchRoot.Cmp(localStateVouchRoot) != 0 {
			log.Debugw(
				"local state roots didn't match the historydb state roots:\n"+
					"localStateAccountRoot: %v vs historydb stateAccountRoot: %v \n"+
					"localStateVouchRoot: %v vs historydb stateVouchRoot: %v \n"+
					"localStateScoreRoot: %v vs historydb stateScoreRoot: %v \n"+
					"Forcing reset from Synchronizer",
				localStateAccountRoot, batch.AccountRoot,
				localStateVouchRoot, batch.VouchRoot,
				localStateScoreRoot, batch.ScoreRoot,
			)
			// StateRoots from synchronizer doesn't match StateRoots
			// from batchBuilder, force a reset from synchronizer
			if err := p.txSelector.Reset(p.state.batchNum, true); err != nil {
				return common.Wrap(err)
			}
			if err := p.batchBuilder.Reset(p.state.batchNum, true); err != nil {
				return common.Wrap(err)
			}
		}
	}
	return nil
}

func (p *Pipeline) syncSCVars(vars common.SCVariablesPtr) {
	updateSCVars(&p.vars, vars)
}

func (p *Pipeline) getErrAtBatchNum() common.BatchNum {
	p.rw.RLock()
	defer p.rw.RUnlock()
	return p.errAtBatchNum
}

// handleForgeBatch waits for an available proof server, calls p.forgeBatch to
// forge the batch and get the zkInputs, and then  sends the zkInputs to the
// selected proof server so that the proof computation begins.

func (p *Pipeline) handleForgeBatch(
	ctx context.Context,
	batchNum common.BatchNum,
) (batchInfo *BatchInfo, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Forge the batch internally (make a selection of txs and prepare
	// all the smart contract arguments)
	var skipReason *string
	batchInfo, skipReason, err = p.forgeBatch(batchNum)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	} else if err != nil {
		log.Errorw("forgeBatch", "err", err)
		return nil, common.Wrap(err)
	} else if skipReason != nil {
		log.Debugw("skipping batch", "batch", batchNum, "reason", *skipReason)
		return nil, common.Wrap(errSkipBatchByPolicy)
	}

	// Send the ZKInputs to the proof server
	batchInfo.ServerProof = p.prover
	batchInfo.ProofStart = time.Now()
	if err := p.sendServerProof(ctx, batchInfo); ctx.Err() != nil {
		return nil, ctx.Err()
	} else if err != nil {
		log.Errorw("sendServerProof", "err", err)
		return nil, common.Wrap(err)
	}
	return batchInfo, nil
}

// sendServerProof sends the circuit inputs to the proof server
func (p *Pipeline) sendServerProof(ctx context.Context, batchInfo *BatchInfo) error {
	p.cfg.debugBatchStore(batchInfo)

	// Call the server proof with BatchBuilder output,
	// save server proof info for batchNum
	if err := batchInfo.ServerProof.CalculateProof(ctx, batchInfo.ZKInputs); err != nil {
		return common.Wrap(err)
	}
	return nil
}

// forgeBatch forges the batchNum batch.
func (p *Pipeline) forgeBatch(batchNum common.BatchNum) (
	batchInfo *BatchInfo,
	skipReason *string,
	err error,
) {
	// Structure to accumulate data and metadata of the batch
	now := time.Now()
	batchInfo = &BatchInfo{PipelineNum: p.num, BatchNum: batchNum}
	batchInfo.Debug.StartTimestamp = now
	batchInfo.Debug.StartBlockNum = p.stats.Eth.LastBlock.Num + 1

	var l1UserTxs []common.L1Tx
	var auths [][]byte

	_l1UserTxs, err := p.historyDB.GetUnforgedL1UserTxs(p.state.lastForgeL1TxsNum + 1)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}
	// l1UserFutureTxs are the l1UserTxs that are not being forged
	// in the next batch, but that are also in the queue for the
	// future batches
	l1UserFutureTxs, err := p.historyDB.GetUnforgedL1UserFutureTxs(p.state.lastForgeL1TxsNum + 1)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}

	// TODO: figure out what happens here and potentially remove txSelector
	auths, l1UserTxs, err =
		p.txSelector.GetL1TxSelection(p.cfg.TxProcessorConfig, _l1UserTxs, l1UserFutureTxs)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}

	// TODO: depending on what's happening in txSelector, this might not be necessary as well
	if skip, reason, err := p.forgePolicySkipPostSelection(now, l1UserTxs, batchInfo); err != nil {
		return nil, nil, common.Wrap(err)
	} else if skip {
		if err := p.txSelector.Reset(batchInfo.BatchNum-1, false); err != nil {
			return nil, nil, common.Wrap(err)
		}
		return nil, &reason, common.Wrap(err)
	}

	p.state.lastScheduledL1BatchBlockNum = p.stats.Eth.LastBlock.Num + 1
	p.state.lastForgeL1TxsNum++

	// Save metadata from TxSelector output for BatchNum
	batchInfo.L1UserTxs = l1UserTxs
	batchInfo.L1CoordinatorTxsAuths = auths

	// Call BatchBuilder with TxSelector output
	configBatch := &batchbuilder.ConfigBatch{
		TxProcessorConfig: p.cfg.TxProcessorConfig,
	}
	zkInputs, err := p.batchBuilder.BuildBatch(configBatch, l1UserTxs)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}

	// Save metadata from BatchBuilder output for BatchNum
	batchInfo.ZKInputs = zkInputs
	batchInfo.Debug.Status = StatusForged
	p.cfg.debugBatchStore(batchInfo)
	log.Infow("Pipeline: batch forged internally", "batch", batchInfo.BatchNum)

	return batchInfo, nil, nil
}

// forgePolicySkipPostSelection is called after doing a tx selection in a batch to
// determine by policy if we should forge the batch or not.  Returns true and
// the reason when the forging of the batch must be skipped.
func (p *Pipeline) forgePolicySkipPostSelection(
	now time.Time,
	l1UserTxsExtra []common.L1Tx,
	batchInfo *BatchInfo,
) (bool, string, error) {
	pendingTxs := true
	if len(l1UserTxsExtra) == 0 {
		// Query the number of unforged L1UserTxs
		// (either in a open queue or in a frozen
		// not-yet-forged queue).
		count, err := p.historyDB.GetUnforgedL1UserTxsCount()
		if err != nil {
			return false, "", err
		}
		// If there are future L1UserTxs, we forge a
		// batch to advance the queues to be able to
		// forge the L1UserTxs in the future.
		// Otherwise, skip.
		if count == 0 {
			pendingTxs = false
		}
	}

	if pendingTxs {
		return false, "", nil
	}
	return true, "no pending txs", nil
}

func (p *Pipeline) setErrAtBatchNum(batchNum common.BatchNum) {
	p.rw.Lock()
	defer p.rw.Unlock()
	p.errAtBatchNum = batchNum
}

// TODO: implement
// waitServerProof gets the generated zkProof & sends it to the SmartContract
func (p *Pipeline) waitServerProof(ctx context.Context, batchInfo *BatchInfo) error {
	// TODO: implement prometheus metrics
	// defer metric.MeasureDuration(metric.WaitServerProof, batchInfo.ProofStart,
	// 	batchInfo.BatchNum.BigInt().String(), strconv.Itoa(batchInfo.PipelineNum))

	// proof, pubInputs, err := batchInfo.ServerProof.GetProof(ctx) // blocking call,
	// // until not resolved don't continue. Returns when the proof server has calculated the proof
	// if err != nil {
	// 	return common.Wrap(err)
	// }
	// batchInfo.Proof = proof
	// batchInfo.PublicInputs = pubInputs
	// batchInfo.ForgeBatchArgs = prepareForgeBatchArgs(batchInfo)
	// batchInfo.Debug.Status = StatusProof
	// p.cfg.debugBatchStore(batchInfo)
	// log.Infow("Pipeline: batch proof calculated", "batch", batchInfo.BatchNum)
	return nil
}

// Start the forging pipeline
func (p *Pipeline) Start(
	batchNum common.BatchNum,
	stats *synchronizer.Stats,
	vars *common.SCVariables,
) error {
	if p.started {
		log.Fatal("Pipeline already started")
	}
	p.started = true

	if err := p.reset(batchNum, stats, vars); err != nil {
		return common.Wrap(err)
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())

	queueSize := 1
	batchChSentServerProof := make(chan *BatchInfo, queueSize)

	p.wg.Add(1)
	go func() {
		timer := time.NewTimer(zeroDuration)
		for {
			select {
			case <-p.ctx.Done():
				log.Infow("Pipeline forgeBatch loop done")
				p.wg.Done()
				return
			case statsVars := <-p.statsVarsCh:
				p.stats = statsVars.Stats
				p.syncSCVars(statsVars.Vars)
			case <-timer.C:
				timer.Reset(p.cfg.ForgeRetryInterval)
				// Once errAtBatchNum != 0, we stop forging
				// batches because there's been an error and we
				// wait for the pipeline to be stopped.
				if p.getErrAtBatchNum() != 0 {
					continue
				}
				batchNum = p.state.batchNum + 1
				batchInfo, err := p.handleForgeBatch(p.ctx, batchNum)
				if p.ctx.Err() != nil {
					continue
				} else if common.Unwrap(err) == errSkipBatchByPolicy {
					continue
				} else if err != nil {
					p.setErrAtBatchNum(batchNum)
					p.coord.SendMsg(p.ctx, MsgStopPipeline{
						Reason: fmt.Sprintf(
							"Pipeline.handleForgBatch: %v", err),
						FailedBatchNum: batchNum,
					})
					continue
				}
				p.lastForgeTime = time.Now()

				p.state.batchNum = batchNum
				select {
				case batchChSentServerProof <- batchInfo:
				case <-p.ctx.Done():
				}
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(zeroDuration)
			}
		}
	}()

	p.wg.Add(1)
	go func() {
		for {
			select {
			case <-p.ctx.Done():
				log.Info("Pipeline waitServerProofSendEth loop done")
				p.wg.Done()
				return
			case batchInfo := <-batchChSentServerProof:
				go func(p *Pipeline, batchInfo *BatchInfo, batchNum common.BatchNum) {
					// Once errAtBatchNum != 0, we stop forging
					// batches because there's been an error and we
					// wait for the pipeline to be stopped.
					if p.getErrAtBatchNum() != 0 {
						return
					}
					err := p.waitServerProof(p.ctx, batchInfo)
					if p.ctx.Err() != nil {
						return
					} else if err != nil {
						log.Errorw("waitServerProof", "err", err)
						p.setErrAtBatchNum(batchInfo.BatchNum)
						p.coord.SendMsg(p.ctx, MsgStopPipeline{
							Reason: fmt.Sprintf(
								"Pipeline.waitServerProof: %v", err),
							FailedBatchNum: batchInfo.BatchNum,
						})
						return
					}
					p.txManager.AddBatch(p.ctx, batchInfo)
				}(p, batchInfo, batchNum)
			}
		}
	}()
	return nil
}

// SetSyncStatsVars is a thread safe method to set the synchronizer Stats
func (p *Pipeline) SetSyncStatsVars(
	ctx context.Context,
	stats *synchronizer.Stats,
	vars *common.SCVariablesPtr,
) {
	select {
	case p.statsVarsCh <- statsVars{Stats: *stats, Vars: *vars}:
	case <-ctx.Done():
	}
}
