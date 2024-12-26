/*
Package txselector is responsible to choose the transactions from the pool that will be forged in the next batch.
The main goal is to come with the most profitable selection, always respecting the constrains of the protocol.
This constrains can be splitted in two categories:

Batch constrains (this information is passed to the txselector as `selectionConfig txprocessor.Config`):
- NLevels: limit the amount of accounts that can be created by NLevels^2 -1.
Note that this constraint is not properly checked, if this situation is reached the entire selection will fail
- MaxFeeTx: maximum amount of different coordinator accounts (idx) that can be used to collect fees.
Note that this constraint is not checked right now, if this situation is reached the `txprocessor` will fail later on preparing the `zki`
- MaxTx: maximum amount of transactions that can fit in a batch, in other words: len(l1UserTxs) + len(l1CoordinatorTxs) + len(l2Txs) <= MaxTx
- MaxL1Tx: maximum amount of L1 transactions that can fit in a batch, in other words: len(l1UserTxs) + len(l1CoordinatorTxs) <= MaxL1Tx

Transaction constrains (this takes into consideration the txs fetched from the pool and the current state stored in StateDB):
- Sender account exists: `FromIdx` must exist in the `StateDB`, and `tx.TokenID == account.TokenID` has to be respected
- Sender account has enough balance: tx.Amount + fee <= account.Balance
- Sender account has correct nonce: `tx.Nonce == account.Nonce`
- Recipient account exists or can be created:
  - In case of transfer to Idx: account MUST already exist on StateDB, and match the `tx.TokenID`
  - In case of transfer to Ethereum address: if the account doesn't exists, it can be created through a `l1CoordinatorTx` IF there is a valid `AccountCreationAuthorization`
  - In case of transfer to BJJ: if the account doesn't exists, it can be created through a `l1CoordinatorTx` (no need for `AccountCreationAuthorization`)

- Atomic transactions: requested transaction exist and can be linked,
according to the `RqOffset` spec: https://docs.hermez.io/#/developers/protocol/hermez-protocol/circuits/circuits?id=rq-tx-verifier

Important considerations:
- It's assumed that signatures are correct, since they're checked before inserting txs to the pool
- The state is processed sequentially meaning that each tx that is selected affects the state, in other words:
the order in which txs are selected can make other txs became valid or invalid.
This specially relevant for the constrains `Sender account has enough balance` and `Sender account has correct nonce`
- The creation of accounts using `l1CoordinatorTxs` increments the amount of used L1 transactions.
This has to be taken into consideration for the constrain `MaxL1Tx`

Current implementation:

The current approach is simple but effective, specially in a scenario of not having a lot of transactions in the pool most of the time:
0. Process L1UserTxs (this transactions come from the Blockchain and it's mandatory by protocol to forge them)
1. Get transactions from the pool
2. Order transactions by (nonce, fee in USD)
3. Selection loop: iterate over the sorted transactions and split in selected and non selected.
Repeat this process with the non-selected of each iteration until one iteration doesn't return any selected txs
Note that this step is the one that ensures that the constrains are respected.
4. Choose coordinator idxs to collect the fees. Note that `MaxFeeTx` constrain is ignored in this step.
5. Return the selected L2 txs as well as the necessary `l1CoordinatorTxs` the `l1UserTxs`
(this is redundant as the return will always be the same as the input) and the coordinator idxs used to collect fees

The previous flow description doesn't take in consideration the constrain `Atomic transactions`.
This constrain alters the previous step as follow:
- Atomic transactions are grouped into `AtomicGroups`, and each group has an average fee that is used to sort the transactions together with the non atomic transactions,
in a way that all the atomic transactions from the same group preserve the relative order as found in the pool.
This is done in this way because it's assumed that transactions from the same `AtomicGroup`
have the same `AtomicGroupID`, and are ordered with valid `RqOffset` in the pool.
- If one atomic transaction fails to be processed in the `Selection loop`,
the group will be marked as invalid and the entire process will reboot from the beginning with the only difference being that
txs belonging to failed atomic groups will be discarded before reaching the `Selection loop`.
This is done this way because the state is altered sequentially, so if a transaction belonging to an atomic group is selected,
but later on a transaction from the same group can't be selected, the selection will be invalid since there will be a selected tx that depends on a tx that
doesn't exist in the selection. Right now the mechanism that the StateDB has to revert changes is to go back to a previous checkpoint (checkpoints are created per batch).
This limitation forces the txselector to restart from the beginning of the batch selection.
This should be improved once the StateDB has more granular mechanisms to revert the effects of processed txs.
*/
package txselector

// current: very simple version of TxSelector

import (
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/database/kvdb"
	"tokamak-sybil-resistance/database/statedb"
	"tokamak-sybil-resistance/txprocessor"
)

// CoordAccount contains the data of the Coordinator account, that will be used
// to create new transactions of CreateAccountDeposit type to add new TokenID
// accounts for the Coordinator to receive the fees.
// type CoordAccount struct {
// 	Addr                ethCommon.Address
// 	BJJ                 babyjub.PublicKeyComp
// 	AccountCreationAuth []byte // signature in byte array format
// }

// TxSelector implements all the functionalities to select the txs for the next
// batch
type TxSelector struct {
	localAccountsDB *statedb.LocalStateDB

	// coordAccount *CoordAccount
}

// NewTxSelector returns a *TxSelector
func NewTxSelector(
	// coordAccount *CoordAccount,
	dbpath string,
	synchronizerStateDB *statedb.StateDB,
) (*TxSelector, error) {
	localAccountsDB, err := statedb.NewLocalStateDB(
		statedb.Config{
			Path:    dbpath,
			Keep:    kvdb.DefaultKeep,
			Type:    statedb.TypeTxSelector,
			NLevels: 0,
		},
		synchronizerStateDB) // without merkletree
	if err != nil {
		return nil, common.Wrap(err)
	}

	return &TxSelector{
		localAccountsDB: localAccountsDB,
		// coordAccount:    coordAccount,
	}, nil
}

// LocalAccountsDB returns the LocalStateDB of the TxSelector
func (txsel *TxSelector) LocalAccountsDB() *statedb.LocalStateDB {
	return txsel.localAccountsDB
}

// Reset tells the TxSelector to get it's internal AccountsDB
// from the required `batchNum`
func (txsel *TxSelector) Reset(batchNum common.BatchNum, fromSynchronizer bool) error {
	return common.Wrap(txsel.localAccountsDB.Reset(batchNum, fromSynchronizer))
}

// GetL1L2TxSelection returns the selection of L1 + L2 txs.
// It returns: the CoordinatorIdxs used to receive the fees of the selected
// L2Txs. An array of bytearrays with the signatures of the
// AccountCreationAuthorization of the accounts of the users created by the
// Coordinator with L1CoordinatorTxs of those accounts that does not exist yet
// but there is a transactions to them and the authorization of account
// creation exists. The L1UserTxs, L1CoordinatorTxs, PoolL2Txs that will be
// included in the next batch.
func (txsel *TxSelector) GetL1TxSelection(
	selectionConfig txprocessor.Config,
	l1UserTxs,
	l1UserFutureTxs []common.L1Tx,
) ([][]byte, []common.L1Tx, error) {
	accCreationAuths, l1UserTxs, err := txsel.getL1TxSelection(selectionConfig, l1UserTxs, l1UserFutureTxs)
	return accCreationAuths, l1UserTxs, err
}

// getL1L2TxSelection returns the selection of L1 + L2 txs.
// It returns: the CoordinatorIdxs used to receive the fees of the selected
// L2Txs. An array of bytearrays with the signatures of the
// AccountCreationAuthorization of the accounts of the users created by the
// Coordinator with L1CoordinatorTxs of those accounts that does not exist yet
// but there is a transactions to them and the authorization of account
// creation exists. The L1UserTxs, L1CoordinatorTxs, PoolL2Txs that will be
// included in the next batch.
func (txsel *TxSelector) getL1TxSelection(selectionConfig txprocessor.Config,
	l1UserTxs, l1UserFutureTxs []common.L1Tx) ([][]byte, []common.L1Tx, error) {
	// 	// WIP.0: the TxSelector is not optimized and will need a redesign. The
	// 	// current version is implemented in order to have a functional
	// 	// implementation that can be used ASAP.

	// 	// Steps of this method:
	// 	// - ProcessL1Txs (User txs)
	// 	// - getPendingTxs (forgable directly with current state & not forgable
	// 	// yet)
	// 	// - split between l2TxsForgable & l2TxsNonForgable, where:
	// 	// 	- l2TxsForgable are the txs that are directly forgable with the
	// 	// 	current state
	// 	// 	- l2TxsNonForgable are the txs that are not directly forgable
	// 	// 	with the current state, but that may be forgable once the
	// 	// 	l2TxsForgable ones are processed
	// 	// - for l2TxsForgable, and if needed, for l2TxsNonForgable:
	// 	// 	- sort by Fee & Nonce
	// 	// 	- loop over l2Txs (txsel.processL2Txs)
	// 	// 	        - Fill tx.TokenID tx.Nonce
	// 	// 	        - Check enough Balance on sender
	// 	// 	        - Check Nonce
	// 	// 	        - Create CoordAccount L1CoordTx for TokenID if needed
	// 	// 	                - & ProcessL1Tx of L1CoordTx
	// 	// 	        - Check validity of receiver Account for ToEthAddr / ToBJJ
	// 	// 	        - Create UserAccount L1CoordTx if needed (and possible)
	// 	// 	        - If everything is fine, store l2Tx to selectedTxs & update NoncesMap
	// 	// - Prepare coordIdxsMap & AccumulatedFees
	// 	// - Distribute AccumulatedFees to CoordIdxs
	// 	// - MakeCheckpoint
	// 	failedAtomicGroups := []failedAtomicGroup{}
	// START_SELECTION:
	// 	txselStateDB := txsel.localAccountsDB.StateDB
	// 	tp := txprocessor.NewTxProcessor(txselStateDB, selectionConfig)
	// 	tp.AccumulatedFees = make(map[common.Idx]*big.Int)

	// 	// Process L1UserTxs
	// 	for i := 0; i < len(l1UserTxs); i++ {
	// 		// assumption: l1usertx are sorted by L1Tx.Position
	// 		_, _, _, _, err := tp.ProcessL1Tx(nil, &l1UserTxs[i])
	// 		if err != nil {
	// 			return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 		}
	// 	}

	// 	// Get pending txs from the pool
	// 	// Filter transactions belonging to failed atomic groups
	// 	selectableTxsTmp, discardedTxs := filterFailedAtomicGroups(l2TxsFromDB, failedAtomicGroups)
	// 	// Filter invalid atomic groups
	// 	selectableTxs, discardedTxsTmp := filterInvalidAtomicGroups(selectableTxsTmp)
	// 	discardedTxs = append(discardedTxs, discardedTxsTmp...)

	// 	// in case that length of l2TxsForgable is 0, no need to continue, there
	// 	// is no L2Txs to forge at all
	// 	if len(selectableTxs) == 0 {
	// 		err = tp.StateDB().MakeCheckpoint()
	// 		if err != nil {
	// 			return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 		}

	// 		metric.SelectedL1UserTxs.Set(float64(len(l1UserTxs)))
	// 		metric.SelectedL1CoordinatorTxs.Set(0)
	// 		metric.SelectedL2Txs.Set(0)
	// 		metric.DiscardedL2Txs.Set(float64(len(discardedTxs)))

	// 		return nil, nil, l1UserTxs, nil, nil, discardedTxs, nil
	// 	}

	// 	// Calculate average fee for atomic groups
	// 	atomicFeeMap := calculateAtomicGroupsAverageFee(selectableTxs)

	// 	// Initialize selection arrays

	// 	// Used authorizations in the l1CoordinatorTxs
	var accAuths [][]byte
	// 	// Processed txs for necessary account creation
	// 	// (fees for coordinator or missing destinatary accounts)
	// 	var l1CoordinatorTxs []common.L1Tx
	// 	// Processed txs
	// 	var selectedTxs []common.PoolL2Tx
	// 	// Start selection process
	// 	shouldKeepSelectionProcess := true
	// 	// Order L2 txs. This has to be done just once,
	// 	// as the array will get smaller over iterations, but the order won't be affected
	// 	selectableTxs = sortL2Txs(selectableTxs, atomicFeeMap)
	// 	for shouldKeepSelectionProcess {
	// 		// Process txs and get selection
	// 		iteAccAuths, iteL1CoordinatorTxs, iteSelectedTxs,
	// 			nonSelectedTxs, invalidTxs, failedAtomicGroup, err := txsel.processL2Txs(
	// 			tp,
	// 			selectionConfig,
	// 			len(l1UserTxs)+len(l1CoordinatorTxs), // Already added L1 Txs
	// 			len(selectedTxs),                     // Already added L2 Txs
	// 			l1UserFutureTxs,                      // Used to prevent the creation of unnecessary accounts
	// 			selectableTxs,                        // Txs that can be selected
	// 		)
	// 		if failedAtomicGroup.id != common.EmptyAtomicGroupID {
	// 			// An atomic group failed to be processed
	// 			// after at least one tx from the group already altered the state.
	// 			// Revert state to current batch and start the selection process again,
	// 			// ignoring the txs from the group that failed
	// 			log.Info(err)
	// 			failedAtomicGroups = append(failedAtomicGroups, failedAtomicGroup)
	// 			if err := txsel.localAccountsDB.Reset(
	// 				txsel.localAccountsDB.CurrentBatch(), false,
	// 			); err != nil {
	// 				return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 			}
	// 			goto START_SELECTION
	// 		}
	// 		if err != nil {
	// 			return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 		}
	// 		// Add iteration results to selection arrays
	// 		accAuths = append(accAuths, iteAccAuths...)
	// 		l1CoordinatorTxs = append(l1CoordinatorTxs, iteL1CoordinatorTxs...)
	// 		selectedTxs = append(selectedTxs, iteSelectedTxs...)
	// 		discardedTxs = append(discardedTxs, invalidTxs...)
	// 		// Prepare for next iteration
	// 		if len(iteSelectedTxs) == 0 { // Stop iterating
	// 			// If in this iteration no txs got selected, stop selection process
	// 			shouldKeepSelectionProcess = false
	// 			// Add non selected txs to the discarded array as at this point they won't get selected
	// 			for i := 0; i < len(nonSelectedTxs); i++ {
	// 				discardedTxs = append(discardedTxs, nonSelectedTxs[i])
	// 			}
	// 		} else { // Keep iterating
	// 			// Try to select nonSelected txs in next iteration
	// 			selectableTxs = nonSelectedTxs
	// 		}
	// 	}

	// 	// get CoordIdxsMap for the TokenIDs
	// 	coordIdxsMap := make(map[common.TokenID]common.Idx)
	// 	for i := 0; i < len(selectedTxs); i++ {
	// 		// get TokenID from tx.Sender
	// 		accSender, err := tp.StateDB().GetAccount(selectedTxs[i].FromIdx)
	// 		if err != nil {
	// 			return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 		}
	// 		tokenID := accSender.TokenID

	// 		coordIdx, err := txsel.getCoordIdx(tokenID)
	// 		if err != nil {
	// 			// if err is db.ErrNotFound, should not happen, as all
	// 			// the selectedTxs.TokenID should have a CoordinatorIdx
	// 			// created in the DB at this point
	// 			return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 		}
	// 		coordIdxsMap[tokenID] = coordIdx
	// 	}

	// var coordIdxs []common.Idx
	// 	for _, idx := range coordIdxsMap {
	// 		coordIdxs = append(coordIdxs, idx)
	// 	}
	// 	// sort CoordIdxs
	// 	sort.SliceStable(coordIdxs, func(i, j int) bool {
	// 		return coordIdxs[i] < coordIdxs[j]
	// 	})

	// 	// distribute the AccumulatedFees from the processed L2Txs into the
	// 	// Coordinator Idxs
	// 	for idx, accumulatedFee := range tp.AccumulatedFees {
	// 		cmp := accumulatedFee.Cmp(big.NewInt(0))
	// 		if cmp == 1 { // accumulatedFee>0
	// 			// send the fee to the Idx of the Coordinator for the TokenID
	// 			accCoord, err := txsel.localAccountsDB.GetAccount(idx)
	// 			if err != nil {
	// 				log.Errorw("Can not distribute accumulated fees to coordinator "+
	// 					"account: No coord Idx to receive fee", "idx", idx)
	// 				return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 			}
	// 			accCoord.Balance = new(big.Int).Add(accCoord.Balance, accumulatedFee)
	// 			_, err = txsel.localAccountsDB.UpdateAccount(idx, accCoord)
	// 			if err != nil {
	// 				log.Error(err)
	// 				return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 			}
	// 		}
	// 	}

	// 	err = tp.StateDB().MakeCheckpoint()
	// 	if err != nil {
	// 		return nil, nil, nil, nil, nil, nil, tracerr.Wrap(err)
	// 	}

	// 	metric.SelectedL1CoordinatorTxs.Set(float64(len(l1CoordinatorTxs)))
	// 	metric.SelectedL1UserTxs.Set(float64(len(l1UserTxs)))
	// 	metric.SelectedL2Txs.Set(float64(len(selectedTxs)))
	// 	metric.DiscardedL2Txs.Set(float64(len(discardedTxs)))

	return accAuths, l1UserTxs, nil
}
