package batchbuilder

import (
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/database/kvdb"
	"tokamak-sybil-resistance/database/statedb"
	"tokamak-sybil-resistance/txprocessor"
)

// ConfigCircuit contains the circuit configuration
type ConfigCircuit struct {
	TxsMax       uint64
	L1TxsMax     uint64
	SMTLevelsMax uint64
}

// BatchBuilder implements the batch builder type, which contains the
// functionalities
type BatchBuilder struct {
	localStateDB *statedb.LocalStateDB
}

// ConfigBatch contains the batch configuration
type ConfigBatch struct {
	TxProcessorConfig txprocessor.Config
}

// NewBatchBuilder constructs a new BatchBuilder, and executes the bb.Reset
// method
func NewBatchBuilder(dbpath string, synchronizerStateDB *statedb.StateDB, batchNum common.BatchNum,
	nLevels uint64) (*BatchBuilder, error) {
	localStateDB, err := statedb.NewLocalStateDB(
		statedb.Config{
			Path:    dbpath,
			Keep:    kvdb.DefaultKeep,
			Type:    statedb.TypeBatchBuilder,
			NLevels: int(nLevels),
		},
		synchronizerStateDB)
	if err != nil {
		return nil, common.Wrap(err)
	}

	bb := BatchBuilder{
		localStateDB: localStateDB,
	}

	err = bb.Reset(batchNum, true)
	return &bb, common.Wrap(err)
}

// Reset tells the BatchBuilder to reset it's internal state to the required
// `batchNum`.  If `fromSynchronizer` is true, the BatchBuilder must take a
// copy of the rollup state from the Synchronizer at that `batchNum`, otherwise
// it can just roll back the internal copy.
func (bb *BatchBuilder) Reset(batchNum common.BatchNum, fromSynchronizer bool) error {
	return nil
	//TODO: Check and Update this reseting functionality
	// return tracerr.Wrap(bb.localStateDB.Reset(batchNum, fromSynchronizer))
}

// LocalStateDB returns the underlying LocalStateDB
func (bb *BatchBuilder) LocalStateDB() *statedb.LocalStateDB {
	return bb.localStateDB
}

// BuildBatch takes the transactions and returns the common.ZKInputs of the next batch
func (bb *BatchBuilder) BuildBatch(
	configBatch *ConfigBatch,
	l1usertxs []common.L1Tx,
) (*common.ZKInputs, error) {
	bbStateDB := bb.localStateDB.StateDB
	tp := txprocessor.NewTxProcessor(bbStateDB, configBatch.TxProcessorConfig)

	ptOut, err := tp.ProcessTxs(l1usertxs)
	if err != nil {
		return nil, common.Wrap(err)
	}
	return ptOut.ZKInputs, nil
}
