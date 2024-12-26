package historydb

import (
	"math/big"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/database"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/jmoiron/sqlx"
	"github.com/russross/meddler"
)

// HistoryDB persist the historic of the rollup
type HistoryDB struct {
	dbRead  *sqlx.DB
	dbWrite *sqlx.DB
	// apiConnCon *database.APIConnectionController
}

// NewHistoryDB initialize the DB
func NewHistoryDB(dbRead, dbWrite *sqlx.DB /*, apiConnCon *database.APIConnectionController*/) *HistoryDB {
	return &HistoryDB{
		dbRead:  dbRead,
		dbWrite: dbWrite,
		// apiConnCon: apiConnCon,
	}
}

// DB returns a pointer to the L2DB.db. This method should be used only for
// internal testing purposes.
func (hdb *HistoryDB) DB() *sqlx.DB {
	return hdb.dbWrite
}

// AddBlock insert a block into the DB
func (hdb *HistoryDB) AddBlock(block *common.Block) error { return hdb.addBlock(hdb.dbWrite, block) }
func (hdb *HistoryDB) addBlock(d meddler.DB, block *common.Block) error {
	return common.Wrap(meddler.Insert(d, "block", block))
}

// AddBlocks inserts blocks into the DB
func (hdb *HistoryDB) AddBlocks(blocks []common.Block) error {
	return common.Wrap(hdb.addBlocks(hdb.dbWrite, blocks))
}

func (hdb *HistoryDB) addBlocks(d meddler.DB, blocks []common.Block) error {
	return common.Wrap(database.BulkInsert(
		d,
		`INSERT INTO block (
			eth_block_num,
			timestamp,
			hash
		) VALUES %s;`,
		blocks,
	))
}

// GetBlock retrieve a block from the DB, given a block number
func (hdb *HistoryDB) GetBlock(blockNum int64) (*common.Block, error) {
	block := &common.Block{}
	err := meddler.QueryRow(
		hdb.dbRead, block,
		"SELECT * FROM block WHERE eth_block_num = $1;", blockNum,
	)
	return block, common.Wrap(err)
}

// GetAllBlocks retrieve all blocks from the DB
func (hdb *HistoryDB) GetAllBlocks() ([]common.Block, error) {
	var blocks []*common.Block
	err := meddler.QueryAll(
		hdb.dbRead, &blocks,
		"SELECT * FROM block ORDER BY eth_block_num;",
	)
	return database.SlicePtrsToSlice(blocks).([]common.Block), common.Wrap(err)
}

// GetLastBlock retrieve the block with the highest block number from the DB
func (hdb *HistoryDB) GetLastBlock() (*common.Block, error) {
	block := &common.Block{}
	err := meddler.QueryRow(
		hdb.dbRead, block, "SELECT * FROM block ORDER BY eth_block_num DESC LIMIT 1;",
	)
	return block, common.Wrap(err)
}

// getBlocks retrieve blocks from the DB, given a range of block numbers defined by from and to
func (hdb *HistoryDB) getBlocks(from, to int64) ([]common.Block, error) {
	var blocks []*common.Block
	err := meddler.QueryAll(
		hdb.dbRead, &blocks,
		"SELECT * FROM block WHERE $1 <= eth_block_num AND eth_block_num < $2 ORDER BY eth_block_num;",
		from, to,
	)
	return database.SlicePtrsToSlice(blocks).([]common.Block), common.Wrap(err)
}

// SetInitialSCVars sets the initial state of rollup, auction, wdelayer smart
// contract variables.  This initial state is stored linked to block 0, which
// always exist in the DB and is used to store initialization data that always
// exist in the smart contracts.
func (hdb *HistoryDB) SetInitialSCVars(rollup *common.RollupVariables) error {
	txn, err := hdb.dbWrite.Beginx()
	if err != nil {
		return common.Wrap(err)
	}
	defer func() {
		if err != nil {
			database.Rollback(txn)
		}
	}()
	// Force EthBlockNum to be 0 because it's the block used to link data
	// that belongs to the creation of the smart contracts
	rollup.EthBlockNum = 0
	if err := hdb.setRollupVars(txn, rollup); err != nil {
		return common.Wrap(err)
	}
	return common.Wrap(txn.Commit())
}

func (hdb *HistoryDB) setRollupVars(d meddler.DB, rollup *common.RollupVariables) error {
	return common.Wrap(meddler.Insert(d, "rollup_vars", rollup))
}

// AddCoordinators insert Coordinators into the DB
func (hdb *HistoryDB) AddCoordinators(coordinators []common.Coordinator) error {
	return common.Wrap(hdb.addCoordinators(hdb.dbWrite, coordinators))
}
func (hdb *HistoryDB) addCoordinators(d meddler.DB, coordinators []common.Coordinator) error {
	if len(coordinators) == 0 {
		return nil
	}
	return common.Wrap(database.BulkInsert(
		d,
		"INSERT INTO coordinator (bidder_addr, forger_addr, eth_block_num, url) VALUES %s;",
		coordinators,
	))
}

// GetLastBatch returns the last forged batch
func (hdb *HistoryDB) GetLastBatch() (*common.Batch, error) {
	var batch common.Batch
	err := meddler.QueryRow(
		hdb.dbRead, &batch, `SELECT batch.batch_num, batch.eth_block_num, batch.forger_addr,
		batch.exit_root,
		batch.vouch_root,
		batch.account_root,
		batch.score_root,
		batch.num_accounts, batch.last_idx, batch.forge_l1_txs_num,
		batch.slot_num, batch.total_fees_usd, batch.gas_price, batch.gas_used, batch.ether_price_usd
		FROM batch ORDER BY batch_num DESC LIMIT 1;`,
	)
	return &batch, common.Wrap(err)
}

// GetLastL1BatchBlockNum returns the blockNum of the latest forged l1Batch
func (hdb *HistoryDB) GetLastL1BatchBlockNum() (int64, error) {
	row := hdb.dbRead.QueryRow(`SELECT eth_block_num FROM batch
		WHERE forge_l1_txs_num IS NOT NULL
		ORDER BY batch_num DESC LIMIT 1;`)
	var blockNum int64
	return blockNum, common.Wrap(row.Scan(&blockNum))
}

// GetLastL1TxsNum returns the greatest ForgeL1TxsNum in the DB from forged
// batches.  If there's no batch in the DB (nil, nil) is returned.
func (hdb *HistoryDB) GetLastL1TxsNum() (*int64, error) {
	row := hdb.dbRead.QueryRow("SELECT MAX(forge_l1_txs_num) FROM batch;")
	lastL1TxsNum := new(int64)
	return lastL1TxsNum, common.Wrap(row.Scan(&lastL1TxsNum))
}

// Reorg deletes all the information that was added into the DB after the
// lastValidBlock.  If lastValidBlock is negative, all block information is
// deleted.
func (hdb *HistoryDB) Reorg(lastValidBlock int64) error {
	var err error
	if lastValidBlock < 0 {
		_, err = hdb.dbWrite.Exec("DELETE FROM block;")
	} else {
		_, err = hdb.dbWrite.Exec("DELETE FROM block WHERE eth_block_num > $1;", lastValidBlock)
	}
	return common.Wrap(err)
}

// AddBatch insert a Batch into the DB
func (hdb *HistoryDB) AddBatch(batch *common.Batch) error { return hdb.addBatch(hdb.dbWrite, batch) }
func (hdb *HistoryDB) addBatch(d meddler.DB, batch *common.Batch) error {
	// Insert to DB
	return common.Wrap(meddler.Insert(d, "batch", batch))
}

// AddBatches insert Bids into the DB
func (hdb *HistoryDB) AddBatches(batches []common.Batch) error {
	return common.Wrap(hdb.addBatches(hdb.dbWrite, batches))
}
func (hdb *HistoryDB) addBatches(d meddler.DB, batches []common.Batch) error {
	for i := 0; i < len(batches); i++ {
		if err := hdb.addBatch(d, &batches[i]); err != nil {
			return common.Wrap(err)
		}
	}
	return nil
}

// GetAllBatches retrieve all batches from the DB
func (hdb *HistoryDB) GetAllBatches() ([]common.Batch, error) {
	var batches []*common.Batch
	err := meddler.QueryAll(
		hdb.dbRead, &batches,
		`SELECT batch.batch_num, batch.eth_block_num, batch.forger_addr,
		batch.account_root, batch.vouch_root, batch.score_root, batch.num_accounts, batch.last_idx, batch.exit_root,
		 batch.forge_l1_txs_num, batch.slot_num, batch.total_fees_usd, batch.eth_tx_hash FROM batch
		 ORDER BY item_id;`,
	)
	return database.SlicePtrsToSlice(batches).([]common.Batch), common.Wrap(err)
}

// GetBatches retrieve batches from the DB, given a range of batch numbers defined by from and to
func (hdb *HistoryDB) GetBatches(from, to common.BatchNum) ([]common.Batch, error) {
	var batches []*common.Batch
	err := meddler.QueryAll(
		hdb.dbRead, &batches,
		`SELECT batch_num, eth_block_num, forger_addr,
		account_root, vouch_root, score_root, num_accounts, last_idx, exit_root, forge_l1_txs_num, slot_num, total_fees_usd, gas_price, gas_used, ether_price_usd 
		FROM batch WHERE $1 <= batch_num AND batch_num < $2 ORDER BY batch_num;`,
		from, to,
	)
	return database.SlicePtrsToSlice(batches).([]common.Batch), common.Wrap(err)
}

// GetFirstBatchBlockNumBySlot returns the ethereum block number of the first
// batch within a slot
func (hdb *HistoryDB) GetFirstBatchBlockNumBySlot(slotNum int64) (int64, error) {
	row := hdb.dbRead.QueryRow(
		`SELECT eth_block_num FROM batch
		WHERE slot_num = $1 ORDER BY batch_num ASC LIMIT 1;`, slotNum,
	)
	var blockNum int64
	return blockNum, common.Wrap(row.Scan(&blockNum))
}

// GetLastBatchNum returns the BatchNum of the latest forged batch
func (hdb *HistoryDB) GetLastBatchNum() (common.BatchNum, error) {
	row := hdb.dbRead.QueryRow("SELECT batch_num FROM batch ORDER BY batch_num DESC LIMIT 1;")
	var batchNum common.BatchNum
	return batchNum, common.Wrap(row.Scan(&batchNum))
}

// GetBatch returns the batch with the given batchNum
func (hdb *HistoryDB) GetBatch(batchNum common.BatchNum) (*common.Batch, error) {
	var batch common.Batch
	err := meddler.QueryRow(
		hdb.dbRead, &batch, `SELECT batch.batch_num, batch.eth_block_num, batch.forger_addr,
		batch.account_root,
		batch.score_root,
		batch.vouch_root,
		batch.num_accounts, batch.last_idx, batch.exit_root, batch.forge_l1_txs_num,
		batch.slot_num, batch.total_fees_usd, batch.gas_price, batch.gas_used, batch.ether_price_usd
		FROM batch WHERE batch_num = $1;`,
		batchNum,
	)
	return &batch, common.Wrap(err)
}

// AddExitTree insert Exit tree into the DB
func (hdb *HistoryDB) AddExitTree(exitTree []common.ExitInfo) error {
	return common.Wrap(hdb.addExitTree(hdb.dbWrite, exitTree))
}
func (hdb *HistoryDB) addExitTree(d meddler.DB, exitTree []common.ExitInfo) error {
	if len(exitTree) == 0 {
		return nil
	}
	return common.Wrap(database.BulkInsert(
		d,
		"INSERT INTO exit_tree (batch_num, account_idx, merkle_proof, balance, "+
			"instant_withdrawn, delayed_withdraw_request, delayed_withdrawn) VALUES %s;",
		exitTree,
	))
}

func (hdb *HistoryDB) updateExitTree(d sqlx.Ext, blockNum int64,
	rollupWithdrawals []common.WithdrawInfo) error {
	// , wDelayerWithdrawals []common.WDelayerTransfer) error {
	if len(rollupWithdrawals) == 0 {
		// && len(wDelayerWithdrawals) == 0 {
		return nil
	}
	type withdrawal struct {
		BatchNum         int32  `db:"batch_num"`
		AccountIdx       int32  `db:"account_idx"`
		InstantWithdrawn *int64 `db:"instant_withdrawn"`
		// DelayedWithdrawRequest *int64             `db:"delayed_withdraw_request"`
		// DelayedWithdrawn       *int64             `db:"delayed_withdrawn"`
		Owner *ethCommon.Address `db:"owner"`
		// Token                  *ethCommon.Address `db:"token"`
	}
	withdrawals := make([]withdrawal, len(rollupWithdrawals)) //+len(wDelayerWithdrawals))
	for i := range rollupWithdrawals {
		info := &rollupWithdrawals[i]
		withdrawals[i] = withdrawal{
			BatchNum:   int32(info.NumExitRoot),
			AccountIdx: int32(info.Idx),
		}
		if info.InstantWithdraw {
			withdrawals[i].InstantWithdrawn = &blockNum
		} else {
			// withdrawals[i].DelayedWithdrawRequest = &blockNum
			withdrawals[i].Owner = &info.Owner
			// withdrawals[i].Token = &info.Token
		}
	}
	// for i := range wDelayerWithdrawals {
	// 	info := &wDelayerWithdrawals[i]
	// 	withdrawals[len(rollupWithdrawals)+i] = withdrawal{
	// 		DelayedWithdrawn: &blockNum,
	// 		Owner:            &info.Owner,
	// 		Token:            &info.Token,
	// 	}
	// }
	// In VALUES we set an initial row of NULLs to set the types of each
	// variable passed as argument
	const query string = `
		UPDATE exit_tree e SET
			instant_withdrawn = d.instant_withdrawn,
			owner = d.owner
		FROM (VALUES
			(NULL::::BIGINT, NULL::::BIGINT, NULL::::BIGINT, NULL::::BYTEA),
			(:batch_num,
			 :account_idx,
			 :instant_withdrawn,
			 :owner)
		) as d (batch_num, account_idx, instant_withdrawn, owner)
		WHERE
			(d.batch_num IS NOT NULL AND e.batch_num = d.batch_num AND e.account_idx = d.account_idx);
		`
	if len(withdrawals) > 0 {
		if _, err := sqlx.NamedExec(d, query, withdrawals); err != nil {
			return common.Wrap(err)
		}
	}

	return nil
}

// AddAccounts insert accounts into the DB
func (hdb *HistoryDB) AddAccounts(accounts []common.Account) error {
	return common.Wrap(hdb.addAccounts(hdb.dbWrite, accounts))
}
func (hdb *HistoryDB) addAccounts(d meddler.DB, accounts []common.Account) error {
	if len(accounts) == 0 {
		return nil
	}
	type TestAccounts struct {
		Idx      common.AccountIdx
		BatchNum common.BatchNum
		BJJ      babyjub.PublicKeyComp
		EthAddr  ethCommon.Address
		Nonce    common.Nonce
		Balance  string
	}

	var testAccounts []TestAccounts

	for _, account := range accounts {
		testAccounts = append(testAccounts, TestAccounts{
			Idx:      account.Idx,
			BatchNum: account.BatchNum,
			BJJ:      account.BJJ,
			EthAddr:  account.EthAddr,
			Nonce:    account.Nonce,
			Balance:  account.Balance.String(),
		})
	}

	return common.Wrap(database.BulkInsert(
		d,
		`INSERT INTO account (
			idx,
			batch_num,
			bjj,
			eth_addr,
			nonce,
			balance
		) VALUES %s;`,
		testAccounts,
	))
}

// GetAllAccounts returns a list of accounts from the DB
func (hdb *HistoryDB) GetAllAccounts() ([]common.Account, error) {
	var accs []*common.Account
	err := meddler.QueryAll(
		hdb.dbRead, &accs,
		"SELECT idx, batch_num, bjj, eth_addr FROM account ORDER BY idx;",
	)
	return database.SlicePtrsToSlice(accs).([]common.Account), common.Wrap(err)
}

// AddAccountUpdates inserts accUpdates into the DB
func (hdb *HistoryDB) AddAccountUpdates(accUpdates []common.AccountUpdate) error {
	return common.Wrap(hdb.addAccountUpdates(hdb.dbWrite, accUpdates))
}
func (hdb *HistoryDB) addAccountUpdates(d meddler.DB, accUpdates []common.AccountUpdate) error {
	if len(accUpdates) == 0 {
		return nil
	}
	return common.Wrap(database.BulkInsert(
		d,
		`INSERT INTO account_update (
			eth_block_num,
			batch_num,
			idx,
			nonce,
			balance
		) VALUES %s;`,
		accUpdates,
	))
}

// GetAllAccountUpdates returns all the AccountUpdate from the DB
func (hdb *HistoryDB) GetAllAccountUpdates() ([]common.AccountUpdate, error) {
	var accUpdates []*common.AccountUpdate
	err := meddler.QueryAll(
		hdb.dbRead, &accUpdates,
		"SELECT eth_block_num, batch_num, idx, nonce, balance FROM account_update ORDER BY idx;",
	)
	return database.SlicePtrsToSlice(accUpdates).([]common.AccountUpdate), common.Wrap(err)
}

// AddL1Txs inserts L1 txs to the DB. USD and DepositAmountUSD will be set automatically before storing the tx.
// If the tx is originated by a coordinator, BatchNum must be provided. If it's originated by a user,
// BatchNum should be null, and the value will be setted by a trigger when a batch forges the tx.
// EffectiveAmount and EffectiveDepositAmount are seted with default values by the DB.
func (hdb *HistoryDB) AddL1Txs(l1txs []common.L1Tx) error {
	return common.Wrap(hdb.addL1Txs(hdb.dbWrite, l1txs))
}

// addL1Txs inserts L1 txs to the DB. USD and DepositAmountUSD will be set automatically before storing the tx.
// If the tx is originated by a coordinator, BatchNum must be provided. If it's originated by a user,
// BatchNum should be null, and the value will be setted by a trigger when a batch forges the tx.
// EffectiveAmount and EffectiveDepositAmount are seted with default values by the DB.
func (hdb *HistoryDB) addL1Txs(d meddler.DB, l1txs []common.L1Tx) error {
	if len(l1txs) == 0 {
		return nil
	}
	txs := []txWrite{}
	for i := 0; i < len(l1txs); i++ {
		var af *big.Float
		if l1txs[i].Amount != nil {
			af = new(big.Float).SetInt(l1txs[i].Amount)
		} else {
			// TODO: lang.go >> func parseLine() >> lines 394-399 leave amount as nil
			// if tx type is deposit, which causes a panic here. Need to discuss how to handle this.
			af = new(big.Float).SetInt(big.NewInt(0))
			l1txs[i].Amount = big.NewInt(0)
		}
		amountFloat, _ := af.Float64()
		laf := new(big.Float).SetInt(l1txs[i].DepositAmount)
		depositAmountFloat, _ := laf.Float64()
		var effectiveFromIdx *common.AccountIdx
		if l1txs[i].UserOrigin {
			if l1txs[i].Type != common.TxTypeCreateAccountDeposit {
				effectiveFromIdx = &l1txs[i].FromIdx
			}
		} else {
			effectiveFromIdx = &l1txs[i].EffectiveFromIdx
		}

		txs = append(txs, txWrite{
			// Generic
			IsL1:             true,
			TxID:             l1txs[i].TxID,
			Type:             l1txs[i].Type,
			Position:         l1txs[i].Position,
			FromIdx:          &l1txs[i].FromIdx,
			EffectiveFromIdx: effectiveFromIdx,
			ToIdx:            l1txs[i].ToIdx,
			Amount:           l1txs[i].Amount,
			AmountFloat:      amountFloat,
			BatchNum:         l1txs[i].BatchNum,
			EthBlockNum:      l1txs[i].EthBlockNum,
			// L1
			ToForgeL1TxsNum:    l1txs[i].ToForgeL1TxsNum,
			UserOrigin:         &l1txs[i].UserOrigin,
			FromEthAddr:        &l1txs[i].FromEthAddr,
			FromBJJ:            &l1txs[i].FromBJJ,
			DepositAmount:      l1txs[i].DepositAmount,
			DepositAmountFloat: &depositAmountFloat,
			EthTxHash:          &l1txs[i].EthTxHash,
			L1Fee:              l1txs[i].L1Fee,
		})
	}
	return common.Wrap(hdb.addTxs(d, txs))
}

func (hdb *HistoryDB) addTxs(d meddler.DB, txs []txWrite) error {
	if len(txs) == 0 {
		return nil
	}
	return common.Wrap(database.BulkInsert(
		d,
		`INSERT INTO tx (
			is_l1,
			id,
			type,
			position,
			from_idx,
			effective_from_idx,
			to_idx,
			amount,
			amount_f,
			batch_num,
			eth_block_num,
			to_forge_l1_txs_num,
			user_origin,
			from_eth_addr,
			from_bjj,
			deposit_amount,
			deposit_amount_f,
			eth_tx_hash,
			l1_fee,
			nonce
		) VALUES %s;`,
		txs,
	))
}

// GetAllExits returns all exit from the DB
func (hdb *HistoryDB) GetAllExits() ([]common.ExitInfo, error) {
	var exits []*common.ExitInfo
	err := meddler.QueryAll(
		hdb.dbRead, &exits,
		`SELECT exit_tree.batch_num, exit_tree.account_idx, exit_tree.merkle_proof,
		exit_tree.balance, exit_tree.instant_withdrawn, exit_tree.delayed_withdraw_request,
		exit_tree.delayed_withdrawn FROM exit_tree ORDER BY item_id;`,
	)
	return database.SlicePtrsToSlice(exits).([]common.ExitInfo), common.Wrap(err)
}

// GetAllL1UserTxs returns all L1UserTxs from the DB
func (hdb *HistoryDB) GetAllL1UserTxs() ([]common.L1Tx, error) {
	var txs []*common.L1Tx
	err := meddler.QueryAll(
		hdb.dbRead, &txs,
		`SELECT tx.id, tx.to_forge_l1_txs_num, tx.position, tx.user_origin,
		tx.from_idx, tx.effective_from_idx, tx.from_eth_addr, tx.from_bjj, tx.to_idx,
		tx.amount, (CASE WHEN tx.batch_num IS NULL THEN NULL WHEN tx.amount_success THEN tx.amount ELSE 0 END) AS effective_amount,
		tx.deposit_amount, (CASE WHEN tx.batch_num IS NULL THEN NULL WHEN tx.deposit_amount_success THEN tx.deposit_amount ELSE 0 END) AS effective_deposit_amount,
		tx.eth_block_num, tx.type, tx.batch_num
		FROM tx WHERE is_l1 = TRUE AND user_origin = TRUE ORDER BY item_id;`,
	)
	return database.SlicePtrsToSlice(txs).([]common.L1Tx), common.Wrap(err)
}

// GetAllL1CoordinatorTxs returns all L1CoordinatorTxs from the DB
func (hdb *HistoryDB) GetAllL1CoordinatorTxs() ([]common.L1Tx, error) {
	var txs []*common.L1Tx
	// Since the query specifies that only coordinator txs are returned, it's safe to assume
	// that returned txs will always have effective amounts
	err := meddler.QueryAll(
		hdb.dbRead, &txs,
		`SELECT tx.id, tx.to_forge_l1_txs_num, tx.position, tx.user_origin,
		tx.from_idx, tx.effective_from_idx, tx.from_eth_addr, tx.from_bjj, tx.to_idx,
		tx.amount, tx.amount AS effective_amount,
		tx.deposit_amount, tx.deposit_amount AS effective_deposit_amount,
		tx.eth_block_num, tx.type, tx.batch_num
		FROM tx WHERE is_l1 = TRUE AND user_origin = FALSE ORDER BY item_id;`,
	)
	return database.SlicePtrsToSlice(txs).([]common.L1Tx), common.Wrap(err)
}


// GetUnforgedL1UserTxs gets L1 User Txs to be forged in the L1Batch with toForgeL1TxsNum.
func (hdb *HistoryDB) GetUnforgedL1UserTxs(toForgeL1TxsNum int64) ([]common.L1Tx, error) {
	var txs []*common.L1Tx
	err := meddler.QueryAll(
		hdb.dbRead, &txs, // only L1 user txs can have batch_num set to null
		`SELECT tx.id, tx.to_forge_l1_txs_num, tx.position, tx.user_origin,
		tx.from_idx, tx.from_eth_addr, tx.from_bjj, tx.to_idx,
		tx.amount, NULL AS effective_amount,
		tx.deposit_amount, NULL AS effective_deposit_amount,
		tx.eth_block_num, tx.type, tx.batch_num
		FROM tx WHERE batch_num IS NULL AND to_forge_l1_txs_num = $1
		ORDER BY position;`,
		toForgeL1TxsNum,
	)
	return database.SlicePtrsToSlice(txs).([]common.L1Tx), common.Wrap(err)
}

// GetUnforgedL1UserFutureTxs gets L1 User Txs to be forged after the L1Batch
// with toForgeL1TxsNum (in one of the future batches, not in the next one).
func (hdb *HistoryDB) GetUnforgedL1UserFutureTxs(toForgeL1TxsNum int64) ([]common.L1Tx, error) {
	var txs []*common.L1Tx
	err := meddler.QueryAll(
		hdb.dbRead, &txs, // only L1 user txs can have batch_num set to null
		`SELECT tx.id, tx.to_forge_l1_txs_num, tx.position, tx.user_origin,
		tx.from_idx, tx.from_eth_addr, tx.from_bjj, tx.to_idx,
		tx.amount, NULL AS effective_amount,
		tx.deposit_amount, NULL AS effective_deposit_amount,
		tx.eth_block_num, tx.type, tx.batch_num
		FROM tx WHERE batch_num IS NULL AND to_forge_l1_txs_num > $1
		ORDER BY position;`,
		toForgeL1TxsNum,
	)
	return database.SlicePtrsToSlice(txs).([]common.L1Tx), err
}

// GetUnforgedL1UserTxsCount returns the count of unforged L1Txs (either in
// open or frozen queues that are not yet forged)
func (hdb *HistoryDB) GetUnforgedL1UserTxsCount() (int, error) {
	row := hdb.dbRead.QueryRow(
		`SELECT COUNT(*) FROM tx WHERE batch_num IS NULL;`,
	)
	var count int
	return count, row.Scan(&count)
}

// GetLastTxsPosition for a given to_forge_l1_txs_num
func (hdb *HistoryDB) GetLastTxsPosition(toForgeL1TxsNum int64) (int, error) {
	row := hdb.dbRead.QueryRow(
		"SELECT position FROM tx WHERE to_forge_l1_txs_num = $1 ORDER BY position DESC;",
		toForgeL1TxsNum,
	)
	var lastL1TxsPosition int
	return lastL1TxsPosition, common.Wrap(row.Scan(&lastL1TxsPosition))
}

// GetSCVars returns the rollup, auction and wdelayer smart contracts variables at their last update.
func (hdb *HistoryDB) GetSCVars() (*common.RollupVariables, error) {
	var rollup common.RollupVariables
	if err := meddler.QueryRow(hdb.dbRead, &rollup,
		"SELECT * FROM rollup_vars ORDER BY eth_block_num DESC LIMIT 1;"); err != nil {
		return nil, err
	}
	return &rollup, nil
}

func (hdb *HistoryDB) addBucketUpdates(d meddler.DB, bucketUpdates []common.BucketUpdate) error {
	if len(bucketUpdates) == 0 {
		return nil
	}
	return common.Wrap(database.BulkInsert(
		d,
		`INSERT INTO bucket_update (
		 	eth_block_num,
		 	num_bucket,
		 	block_stamp,
		 	withdrawals
		) VALUES %s;`,
		bucketUpdates,
	))
}

// AddBucketUpdatesTest allows call to unexported method
// only for internal testing purposes
func (hdb *HistoryDB) AddBucketUpdatesTest(d meddler.DB, bucketUpdates []common.BucketUpdate) error {
	return hdb.addBucketUpdates(d, bucketUpdates)
}

// GetAllBucketUpdates retrieves all the bucket updates
func (hdb *HistoryDB) GetAllBucketUpdates() ([]common.BucketUpdate, error) {
	var bucketUpdates []*common.BucketUpdate
	err := meddler.QueryAll(
		hdb.dbRead, &bucketUpdates,
		`SELECT eth_block_num, num_bucket, block_stamp, withdrawals  
		FROM bucket_update ORDER BY item_id;`,
	)
	return database.SlicePtrsToSlice(bucketUpdates).([]common.BucketUpdate), common.Wrap(err)
}

// setExtraInfoForgedL1UserTxs sets the EffectiveAmount, EffectiveDepositAmount
// and EffectiveFromIdx of the given l1UserTxs (with an UPDATE)
func (hdb *HistoryDB) setExtraInfoForgedL1UserTxs(d sqlx.Ext, txs []common.L1Tx) error {
	if len(txs) == 0 {
		return nil
	}
	// Effective amounts are stored as success flags in the DB, with true value by default
	// to reduce the amount of updates. Therefore, only amounts that became uneffective should be
	// updated to become false.  At the same time, all the txs that contain
	// accounts (FromIdx == 0) are updated to set the EffectiveFromIdx.
	type txUpdate struct {
		ID                   common.TxID       `db:"id"`
		AmountSuccess        bool              `db:"amount_success"`
		DepositAmountSuccess bool              `db:"deposit_amount_success"`
		EffectiveFromIdx     common.AccountIdx `db:"effective_from_idx"`
	}
	txUpdates := []txUpdate{}
	equal := func(a *big.Int, b *big.Int) bool {
		return a.Cmp(b) == 0
	}
	for i := range txs {
		amountSuccess := equal(txs[i].Amount, txs[i].EffectiveAmount)
		depositAmountSuccess := equal(txs[i].DepositAmount, txs[i].EffectiveDepositAmount)
		if !amountSuccess || !depositAmountSuccess || txs[i].FromIdx == 0 {
			txUpdates = append(txUpdates, txUpdate{
				ID:                   txs[i].TxID,
				AmountSuccess:        amountSuccess,
				DepositAmountSuccess: depositAmountSuccess,
				EffectiveFromIdx:     txs[i].EffectiveFromIdx,
			})
		}
	}
	const query string = `
		UPDATE tx SET
			amount_success = tx_update.amount_success,
			deposit_amount_success = tx_update.deposit_amount_success,
			effective_from_idx = tx_update.effective_from_idx
		FROM (VALUES
			(NULL::::BYTEA, NULL::::BOOL, NULL::::BOOL, NULL::::BIGINT),
			(:id, :amount_success, :deposit_amount_success, :effective_from_idx)
		) as tx_update (id, amount_success, deposit_amount_success, effective_from_idx)
		WHERE tx.id = tx_update.id;
	`
	if len(txUpdates) > 0 {
		if _, err := sqlx.NamedExec(d, query, txUpdates); err != nil {
			return common.Wrap(err)
		}
	}
	return nil
}

// AddBlockSCData stores all the information of a block retrieved by the
// Synchronizer.  Blocks should be inserted in order, leaving no gaps because
// the pagination system of the API/DB depends on this.  Within blocks, all
// items should also be in the correct order (Accounts, Tokens, Txs, etc.)
func (hdb *HistoryDB) AddBlockSCData(blockData *common.BlockData) (err error) {
	txn, err := hdb.dbWrite.Beginx()
	if err != nil {
		return common.Wrap(err)
	}
	defer func() {
		if err != nil {
			database.Rollback(txn)
		}
	}()

	// Add block
	if err := hdb.addBlock(txn, &blockData.Block); err != nil {
		return common.Wrap(err)
	}

	// // Add Coordinators
	// if err := hdb.addCoordinators(txn, blockData.Auction.Coordinators); err != nil {
	// 	return common.Wrap(err)
	// }

	// // Add Bids
	// if err := hdb.addBids(txn, blockData.Auction.Bids); err != nil {
	// 	return common.Wrap(err)
	// }

	// // Add Tokens
	// if err := hdb.addTokens(txn, blockData.Rollup.AddedTokens); err != nil {
	// 	return common.Wrap(err)
	// }

	// Prepare user L1 txs to be added.
	// They must be added before the batch that will forge them (which can be in the same block)
	// and after the account that will be sent to (also can be in the same block).
	// Note: insert order is not relevant since item_id will be updated by a DB trigger when
	// the batch that forges those txs is inserted
	userL1s := make(map[common.BatchNum][]common.L1Tx)
	for i := range blockData.Rollup.L1UserTxs {
		batchThatForgesIsInTheBlock := false
		for _, batch := range blockData.Rollup.Batches {
			if batch.Batch.ForgeL1TxsNum != nil &&
				*batch.Batch.ForgeL1TxsNum == *blockData.Rollup.L1UserTxs[i].ToForgeL1TxsNum {
				// Tx is forged in this block. It's guaranteed that:
				// * the first batch of the block won't forge user L1 txs that have been added in this block
				// * batch nums are sequential therefore it's safe to add the tx at batch.BatchNum -1
				batchThatForgesIsInTheBlock = true
				addAtBatchNum := batch.Batch.BatchNum - 1
				userL1s[addAtBatchNum] = append(userL1s[addAtBatchNum], blockData.Rollup.L1UserTxs[i])
				break
			}
		}
		if !batchThatForgesIsInTheBlock {
			// User artificial batchNum 0 to add txs that are not forge in this block
			// after all the accounts of the block have been added
			userL1s[0] = append(userL1s[0], blockData.Rollup.L1UserTxs[i])
		}
	}

	// Add Batches
	for i := range blockData.Rollup.Batches {
		batch := &blockData.Rollup.Batches[i]
		batch.Batch.GasPrice = big.NewInt(0)

		// Add Batch: this will trigger an update on the DB
		// that will set the batch num of forged L1 txs in this batch
		if err = hdb.addBatch(txn, &batch.Batch); err != nil {
			return common.Wrap(err)
		}

		// Add accounts
		if err := hdb.addAccounts(txn, batch.CreatedAccounts); err != nil {
			return common.Wrap(err)
		}

		// Add accountBalances if it exists
		if err := hdb.addAccountUpdates(txn, batch.UpdatedAccounts); err != nil {
			return common.Wrap(err)
		}

		// // Set the EffectiveAmount and EffectiveDepositAmount of all the
		// // L1UserTxs that have been forged in this batch
		// if err = hdb.setExtraInfoForgedL1UserTxs(txn, batch.L1UserTxs); err != nil {
		// 	return common.Wrap(err)
		// }

		// Add user L1 txs that will be forged in next batch
		if userlL1s, ok := userL1s[batch.Batch.BatchNum]; ok {
			if err := hdb.addL1Txs(txn, userlL1s); err != nil {
				return common.Wrap(err)
			}
		}

		// Add exit tree
		if err := hdb.addExitTree(txn, batch.ExitTree); err != nil {
			return common.Wrap(err)
		}
	}
	// Add user L1 txs that won't be forged in this block
	if userL1sNotForgedInThisBlock, ok := userL1s[0]; ok {
		if err := hdb.addL1Txs(txn, userL1sNotForgedInThisBlock); err != nil {
			return common.Wrap(err)
		}
	}

	// Set SC Vars if there was an update
	if blockData.Rollup.Vars != nil {
		if err := hdb.setRollupVars(txn, blockData.Rollup.Vars); err != nil {
			return common.Wrap(err)
		}
	}

	// // Update withdrawals in exit tree table
	// if err := hdb.updateExitTree(txn, blockData.Block.Num,
	// 	blockData.Rollup.Withdrawals, blockData.WDelayer.Withdrawals); err != nil {
	// 	return common.Wrap(err)
	// }

	// // Add Escape Hatch Withdrawals
	// if err := hdb.addEscapeHatchWithdrawals(txn,
	// 	blockData.WDelayer.EscapeHatchWithdrawals); err != nil {
	// 	return common.Wrap(err)
	// }

	// // Add Buckets withdrawals updates
	// if err := hdb.addBucketUpdates(txn, blockData.Rollup.UpdateBucketWithdraw); err != nil {
	// 	return common.Wrap(err)
	// }

	return common.Wrap(txn.Commit())
}
