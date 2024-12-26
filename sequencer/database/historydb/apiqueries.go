package historydb

import (
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/database"

	"github.com/russross/meddler"
)

// GetBatchInternalAPI return the batch with the given batchNum
func (hdb *HistoryDB) GetBatchInternalAPI(batchNum common.BatchNum) (*BatchAPI, error) {
	return hdb.getBatchAPI(hdb.dbRead, batchNum)
}

func (hdb *HistoryDB) getBatchAPI(d meddler.DB, batchNum common.BatchNum) (*BatchAPI, error) {
	batch := &BatchAPI{}
	if err := meddler.QueryRow(
		d, batch,
		`SELECT batch.item_id, batch.batch_num, batch.eth_block_num,
		batch.forger_addr, batch.account_root, batch.score_root, batch.vouch_root, batch.num_accounts, batch.exit_root, batch.forge_l1_txs_num,
		COALESCE(batch.eth_tx_hash, DECODE('0000000000000000000000000000000000000000000000000000000000000000', 'hex')) as eth_tx_hash,
		block.timestamp, block.hash, COALESCE ((SELECT COUNT(*) FROM tx WHERE batch_num = batch.batch_num), 0) AS forged_txs
	    FROM batch INNER JOIN block ON batch.eth_block_num = block.eth_block_num
	 	WHERE batch_num = $1;`, batchNum,
	); err != nil {
		return nil, common.Wrap(err)
	}
	return batch, nil
}

// GetBucketUpdatesInternalAPI returns the latest bucket updates
func (hdb *HistoryDB) GetBucketUpdatesInternalAPI() ([]BucketUpdateAPI, error) {
	var bucketUpdates []*BucketUpdateAPI
	err := meddler.QueryAll(
		hdb.dbRead, &bucketUpdates,
		`SELECT num_bucket, withdrawals FROM bucket_update 
			WHERE item_id in(SELECT max(item_id) FROM bucket_update 
			group by num_bucket) 
			ORDER BY num_bucket ASC;`,
	)
	return database.SlicePtrsToSlice(bucketUpdates).([]BucketUpdateAPI), common.Wrap(err)
}
