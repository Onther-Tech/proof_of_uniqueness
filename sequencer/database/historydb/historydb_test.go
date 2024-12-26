package historydb

import (
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/database"
	"tokamak-sybil-resistance/log"
	"tokamak-sybil-resistance/test"
	"tokamak-sybil-resistance/test/til"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var historyDB *HistoryDB

// var historyDBWithACC *HistoryDB

// Block0 represents Ethereum's genesis block,
// which is stored by default at HistoryDB
var Block0 common.Block = common.Block{
	Num: 0,
	Hash: ethCommon.Hash([32]byte{
		212, 229, 103, 64, 248, 118, 174, 248,
		192, 16, 184, 106, 64, 213, 245, 103,
		69, 161, 24, 208, 144, 106, 52, 230,
		154, 236, 140, 13, 177, 203, 143, 163,
	}), // 0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3
	Timestamp: time.Date(2015, time.July, 30, 3, 26, 13, 0, time.UTC), // 2015-07-30 03:26:13
}

func setup(db *HistoryDB, t *testing.T) {
	test.MigrationsUpTest(db.DB())
	t.Cleanup(func() {
		test.MigrationsDownTest(db.DB())
	})
}

func TestMain(m *testing.M) {
	// init DB
	db, err := database.InitSQLDB()
	if err != nil {
		panic(err)
	}
	historyDB = NewHistoryDB(db, db)
	// apiConnCon := database.NewAPIConnectionController(1, time.Second)
	// historyDBWithACC = NewHistoryDB(db, db, apiConnCon)

	test.MigrationsDownTest(historyDB.DB())

	// Run tests
	result := m.Run()
	// Close DB
	if err := db.Close(); err != nil {
		log.Error("Error closing the history DB", err)
	}
	os.Exit(result)
}

func TestBlocks(t *testing.T) {
	setup(historyDB, t)

	var fromBlock, toBlock int64
	fromBlock = 0
	toBlock = 7
	// Generate blocks using til
	set1 := `
		Type: Blockchain
		// block 0 is stored as default in the DB
		// block 1 does not exist
		> block // blockNum=2
		> block // blockNum=3
		> block // blockNum=4
		> block // blockNum=5
		> block // blockNum=6
	`
	tc := til.NewContext(uint64(0), 1)
	blocks, err := tc.GenerateBlocks(set1)
	require.NoError(t, err)
	// Save timestamp of a block with UTC and change it without UTC
	timestamp := time.Now().Add(time.Second * 13)
	blocks[fromBlock].Block.Timestamp = timestamp
	// Insert blocks into DB
	for i := 0; i < len(blocks); i++ {
		err := historyDB.AddBlock(&blocks[i].Block)
		assert.NoError(t, err)
	}
	// Add block 0 to the generated blocks
	blocks = append(
		[]common.BlockData{{Block: Block0}}, //nolint:gofmt
		blocks...,
	)
	// Get all blocks from DB
	fetchedBlocks, err := historyDB.getBlocks(fromBlock, toBlock)
	assert.Equal(t, len(blocks), len(fetchedBlocks))
	// Compare generated vs getted blocks
	assert.NoError(t, err)
	for i := range fetchedBlocks {
		assertEqualBlock(t, &blocks[i].Block, &fetchedBlocks[i])
	}
	// Compare saved timestamp vs getted
	nameZoneUTC, offsetUTC := timestamp.UTC().Zone()
	zoneFetchedBlock, offsetFetchedBlock := fetchedBlocks[fromBlock].Timestamp.Zone()
	assert.Equal(t, nameZoneUTC, zoneFetchedBlock)
	assert.Equal(t, offsetUTC, offsetFetchedBlock)
	// Get blocks from the DB one by one
	for i := int64(2); i < toBlock; i++ { // avoid block 0 for simplicity
		fetchedBlock, err := historyDB.GetBlock(i)
		assert.NoError(t, err)
		assertEqualBlock(t, &blocks[i-1].Block, fetchedBlock)
	}
	// Get last block
	lastBlock, err := historyDB.GetLastBlock()
	assert.NoError(t, err)
	assertEqualBlock(t, &blocks[len(blocks)-1].Block, lastBlock)
}

func TestBatches(t *testing.T) {
	setup(historyDB, t)

	// Generate batches using til (and blocks for foreign key)
	set := `
		Type: Blockchain
		
		CreateAccountDeposit A: 2000
		CreateAccountDeposit B: 1000
		> batchL1
		> batchL1
		CreateVouch A-B
		CreateVouch B-A
		> batchL1   // batchNum=2, L2 only batch, forges createVouches
		> block
		DeleteVouch A-B
		> batchL1   // batchNum=3, L2 only batch, forges deleteVouch
		DeleteVouch B-A
		> batchL1   // batchNum=4, L2 only batch, forges delteVouch
		> block
	`
	tc := til.NewContext(uint64(0), common.RollupConstMaxL1UserTx)
	tilCfgExtra := til.ConfigExtra{
		BootCoordAddr: ethCommon.HexToAddress("0xE39fEc6224708f0772D2A74fd3f9055A90E0A9f2"),
		CoordUser:     "A",
	}
	blocks, err := tc.GenerateBlocks(set)
	require.NoError(t, err)
	err = tc.FillBlocksExtra(blocks, &tilCfgExtra)
	require.NoError(t, err)
	// Insert to DB
	batches := []common.Batch{}
	lastL1TxsNum := new(int64)
	lastL1BatchBlockNum := int64(0)
	for _, block := range blocks {
		// Insert block
		assert.NoError(t, historyDB.AddBlock(&block.Block))
		// Combine all generated batches into single array
		for _, batch := range block.Rollup.Batches {
			batch.Batch.GasPrice = big.NewInt(0)
			batches = append(batches, batch.Batch)
			forgeTxsNum := batch.Batch.ForgeL1TxsNum
			if forgeTxsNum != nil && (*lastL1TxsNum < *forgeTxsNum) {
				*lastL1TxsNum = *forgeTxsNum
				lastL1BatchBlockNum = batch.Batch.EthBlockNum
			}
		}
	}
	// Insert batches
	assert.NoError(t, historyDB.AddBatches(batches))

	// Get batches from the DB
	fetchedBatches, err := historyDB.GetBatches(0, common.BatchNum(len(batches)+1))
	assert.NoError(t, err)
	assert.Equal(t, len(batches), len(fetchedBatches))
	for i, fetchedBatch := range fetchedBatches {
		assert.Equal(t, batches[i], fetchedBatch)
	}
	// Test GetLastBatchNum
	fetchedLastBatchNum, err := historyDB.GetLastBatchNum()
	assert.NoError(t, err)
	assert.Equal(t, batches[len(batches)-1].BatchNum, fetchedLastBatchNum)
	// Test GetLastBatch
	fetchedLastBatch, err := historyDB.GetLastBatch()
	assert.NoError(t, err)
	assert.Equal(t, &batches[len(batches)-1], fetchedLastBatch)
	// Test GetLastL1TxsNum
	fetchedLastL1TxsNum, err := historyDB.GetLastL1TxsNum()
	assert.NoError(t, err)
	assert.Equal(t, lastL1TxsNum, fetchedLastL1TxsNum)
	// Test GetLastL1BatchBlockNum
	fetchedLastL1BatchBlockNum, err := historyDB.GetLastL1BatchBlockNum()
	assert.NoError(t, err)
	assert.Equal(t, lastL1BatchBlockNum, fetchedLastL1BatchBlockNum)
	// Test GetBatch
	fetchedBatch, err := historyDB.GetBatch(1)
	require.NoError(t, err)
	assert.Equal(t, &batches[0], fetchedBatch)
	_, err = historyDB.GetBatch(common.BatchNum(len(batches) + 1))
	assert.Equal(t, sql.ErrNoRows, common.Unwrap(err))
}

func TestAccounts(t *testing.T) {
	setup(historyDB, t)

	const fromBlock int64 = 1
	const toBlock int64 = 5

	// Prepare blocks in the DB
	blocks := setTestBlocks(fromBlock, toBlock)

	// Generate fake batches
	const nBatches = 10
	batches := test.GenBatches(nBatches, blocks)
	err := historyDB.AddBatches(batches)
	assert.NoError(t, err)

	// Generate fake accounts
	const nAccounts = 3
	accs := test.GenAccounts(nAccounts, 0, nil, nil, batches)
	err = historyDB.AddAccounts(accs)
	assert.NoError(t, err)

	// Fetch accounts
	fetchedAccs, err := historyDB.GetAllAccounts()
	assert.NoError(t, err)

	// Compare fetched accounts vs generated accounts
	for i, acc := range fetchedAccs {
		accs[i].Balance = nil
		assert.Equal(t, accs[i], acc)
	}

	// Test AccountBalances
	accUpdates := make([]common.AccountUpdate, len(accs))
	for i, acc := range accs {
		accUpdates[i] = common.AccountUpdate{
			EthBlockNum: batches[acc.BatchNum-1].EthBlockNum,
			BatchNum:    acc.BatchNum,
			Idx:         acc.Idx,
			Nonce:       common.Nonce(i),
			Balance:     big.NewInt(int64(i)),
		}
	}
	err = historyDB.AddAccountUpdates(accUpdates)
	require.NoError(t, err)
	fetchedAccBalances, err := historyDB.GetAllAccountUpdates()
	require.NoError(t, err)
	assert.Equal(t, accUpdates, fetchedAccBalances)
}

func TestTxs(t *testing.T) {
	setup(historyDB, t)

	set := `
	Type: Blockchain
	
	CreateAccountDeposit A: 10 	// L1Tx 1
	CreateAccountDeposit B: 20	// L1Tx 2
	> batchL1					// batch 1
	> batchL1					// batch 2

	CreateVouch A-B		// L1Tx 3
	CreateVouch B-A		// L1Tx 4
	> batchL1				// batch 3
	> block  			// block 1
	
	Deposit B: 10	// L1Tx 5
	// Exit A: 10		// L2Tx 3
	> batchL1			// batch 4
	> block  		// block 2
	
	ForceExit A: 5		// L1Tx 6
	> batchL1			// batch 5
	> batchL1			// batch 6
	> block				// block 3

	CreateAccountDeposit C: 10		// L1Tx 7
	> batchL1						// batch 7
	> block  						// block 4
	
	CreateAccountDeposit D: 10		// L1Tx 8
	> batchL1						// batch 8
	> batchL1						// batch 9
	> block							// block 5
	
	CreateVouch A-C		// L1Tx 9
	CreateVouch C-D		// L1Tx 10
	DeleteVouch B-A		// L1Tx 11
	> batchL1				// batch 10
	> block 			// block 6

	// CreateAccountDeposit E: 10		// L1Tx 12
	// > batchL1						// batch 11
	// > batchL1						// batch 12
	// > block							// block 7
`

	tc := til.NewContext(uint64(0), common.RollupConstMaxL1UserTx)
	tilCfgExtra := til.ConfigExtra{
		BootCoordAddr: ethCommon.HexToAddress("0xE39fEc6224708f0772D2A74fd3f9055A90E0A9f2"),
		CoordUser:     "A",
	}
	blocks, err := tc.GenerateBlocks(set)
	assert.NoError(t, err)

	err = tc.FillBlocksExtra(blocks, &tilCfgExtra)
	assert.NoError(t, err)

	// Sanity check
	assert.Equal(t, 6, len(blocks)) // total number of blocks is 6

	assert.Equal(t, 4, len(blocks[0].Rollup.L1UserTxs))                  // block 1 contains 4 L1UserTxs
	assert.Equal(t, 3, len(blocks[0].Rollup.Batches))                    // block 1 contains 2 Batches
	assert.Equal(t, 2, len(blocks[0].Rollup.Batches[1].CreatedAccounts)) // block 1, batch 2 contains 2 CreatedAccounts
	// assert.Equal(t, 2, len(blocks[0].Rollup.Batches[2].L2Txs))           // block 1, batch 1 contains 2 L2Txs

	assert.Equal(t, 1, len(blocks[1].Rollup.L1UserTxs)) // block 2 contains 1 L1UserTxs
	assert.Equal(t, 1, len(blocks[1].Rollup.Batches))   // block 2 contains 1 Batch
	// assert.Equal(t, 1, len(blocks[1].Rollup.Batches[0].L2Txs)) // block 2, batch 1 contains 1 L2Tx

	assert.Equal(t, 2, len(blocks[2].Rollup.Batches))   // block 3 contains 2 Batches
	assert.Equal(t, 1, len(blocks[2].Rollup.L1UserTxs)) // block 3 contains 1 L1UserTxs
	// assert.Equal(t, 0, len(blocks[2].Rollup.Batches[1].L2Txs)) // block 3, batch 2 contains 0 L2Tx

	assert.Equal(t, 1, len(blocks[3].Rollup.Batches))   // block 4 contains 1 Batch
	assert.Equal(t, 1, len(blocks[3].Rollup.L1UserTxs)) // block 4 contains 1 L1UserTxs
	// assert.Equal(t, 0, len(blocks[3].Rollup.Batches[0].L2Txs)) // block 4, batch 2 contains 0 L2Tx

	assert.Equal(t, 2, len(blocks[4].Rollup.Batches))   // block 5 contains 2 Batches
	assert.Equal(t, 1, len(blocks[4].Rollup.L1UserTxs)) // block 5 contains 1 L1UserTxs
	// assert.Equal(t, 0, len(blocks[4].Rollup.Batches[0].L2Txs)) // block 5, batch 1 contains 0 L2Tx

	assert.Equal(t, 1, len(blocks[5].Rollup.Batches))   // block 6 contains 1 Batch
	assert.Equal(t, 3, len(blocks[5].Rollup.L1UserTxs)) // block 6 contains 3 L1UserTxs
	// assert.Equal(t, 0, len(blocks[5].Rollup.Batches[0].L2Txs)) // block 6, batch 1 contains 3 L2Tx

	// assert.Equal(t, 2, len(blocks[6].Rollup.Batches))          // block 7 contains 1 Batch
	// assert.Equal(t, 1, len(blocks[6].Rollup.L1UserTxs))        // block 7 contains 0 L1UserTxs
	// assert.Equal(t, 0, len(blocks[6].Rollup.Batches[0].L2Txs)) // block 7, batch 1 contains 3 L2Tx

	var null *common.BatchNum = nil

	// Insert blocks into DB
	for i := range blocks {
		// if i == len(blocks)-1 {
		// 	blocks[i].Block.Timestamp = time.Now()
		// 	dbL1Txs, err := historyDB.GetAllL1UserTxs()
		// 	assert.NoError(t, err)
		// 	// Check batch_num is nil before forging
		// 	assert.Equal(t, null, dbL1Txs[len(dbL1Txs)-1].BatchNum)
		// }
		err = historyDB.AddBlockSCData(&blocks[i])
		assert.NoError(t, err)
	}

	// Check blocks
	dbBlocks, err := historyDB.GetAllBlocks()
	assert.NoError(t, err)
	assert.Equal(t, len(blocks)+1, len(dbBlocks))

	// Check batches
	batches, err := historyDB.GetAllBatches()
	assert.NoError(t, err)
	assert.Equal(t, 10, len(batches))

	// Check L1 Transactions
	dbL1Txs, err := historyDB.GetAllL1UserTxs()
	assert.NoError(t, err)
	assert.Equal(t, 11, len(dbL1Txs))

	// Tx Type
	assert.Equal(t, common.TxTypeCreateAccountDeposit, dbL1Txs[0].Type)
	assert.Equal(t, common.TxTypeCreateAccountDeposit, dbL1Txs[1].Type)
	assert.Equal(t, common.TxTypeCreateVouch, dbL1Txs[2].Type)
	assert.Equal(t, common.TxTypeCreateVouch, dbL1Txs[3].Type)
	assert.Equal(t, common.TxTypeDeposit, dbL1Txs[4].Type)
	assert.Equal(t, common.TxTypeForceExit, dbL1Txs[5].Type)
	assert.Equal(t, common.TxTypeCreateAccountDeposit, dbL1Txs[6].Type)
	assert.Equal(t, common.TxTypeCreateAccountDeposit, dbL1Txs[7].Type)
	assert.Equal(t, common.TxTypeCreateVouch, dbL1Txs[8].Type)
	assert.Equal(t, common.TxTypeCreateVouch, dbL1Txs[9].Type)
	assert.Equal(t, common.TxTypeDeleteVouch, dbL1Txs[10].Type)

	// Tx ID
	assert.Equal(t, "0x00e979da4b80d60a17ce56fa19278c6f3a7e1b43359fb8a8ea46d0264de7d653ab", dbL1Txs[0].TxID.String())
	assert.Equal(t, "0x00af9bf96eb60f2d618519402a2f6b07057a034fa2baefd379fe8e1c969f1c5cf4", dbL1Txs[1].TxID.String())
	assert.Equal(t, "0x00a256ee191905243320ea830840fd666a73c7b4e6f89ce4bd47ddf998dfee627a", dbL1Txs[2].TxID.String())
	assert.Equal(t, "0x007f5383186254f364bfe82ef3ccff4b1bf532bfb1d424fe3858e492b61b0262fe", dbL1Txs[3].TxID.String())
	assert.Equal(t, "0x005d7ee63390a6c95f23a97607f0b66edd44b7999f9a14a16d92107c02baee0700", dbL1Txs[4].TxID.String())
	assert.Equal(t, "0x00930696d03ae0a1e6150b6ccb88043cb539a4e06a7f8baf213029ce9a0600197e", dbL1Txs[5].TxID.String())

	// Tx From IDx
	assert.Equal(t, common.AccountIdx(0), dbL1Txs[0].FromIdx)
	assert.Equal(t, common.AccountIdx(0), dbL1Txs[1].FromIdx)
	assert.NotEqual(t, common.AccountIdx(0), dbL1Txs[2].FromIdx)
	assert.NotEqual(t, common.AccountIdx(0), dbL1Txs[3].FromIdx)
	assert.NotEqual(t, common.AccountIdx(0), dbL1Txs[4].FromIdx)
	assert.NotEqual(t, common.AccountIdx(0), dbL1Txs[5].FromIdx)
	assert.Equal(t, common.AccountIdx(0), dbL1Txs[6].FromIdx)
	assert.Equal(t, common.AccountIdx(0), dbL1Txs[7].FromIdx)
	assert.NotEqual(t, common.AccountIdx(0), dbL1Txs[8].FromIdx)
	assert.NotEqual(t, common.AccountIdx(0), dbL1Txs[9].FromIdx)
	assert.NotEqual(t, common.AccountIdx(0), dbL1Txs[10].FromIdx)

	// Batch Number
	var bn = common.BatchNum(2)

	assert.Equal(t, &bn, dbL1Txs[0].BatchNum)
	assert.Equal(t, &bn, dbL1Txs[1].BatchNum)

	bn = common.BatchNum(4)
	assert.Equal(t, bn, *dbL1Txs[2].BatchNum)
	assert.Equal(t, bn, *dbL1Txs[3].BatchNum)

	bn = common.BatchNum(6)
	assert.Equal(t, &bn, dbL1Txs[5].BatchNum)

	// eth_block_num
	assert.Equal(t, int64(2), dbL1Txs[0].EthBlockNum)
	assert.Equal(t, int64(2), dbL1Txs[1].EthBlockNum)
	assert.Equal(t, int64(2), dbL1Txs[2].EthBlockNum)
	assert.Equal(t, int64(2), dbL1Txs[3].EthBlockNum)
	assert.Equal(t, int64(3), dbL1Txs[4].EthBlockNum)
	assert.Equal(t, int64(4), dbL1Txs[5].EthBlockNum)
	assert.Equal(t, int64(5), dbL1Txs[6].EthBlockNum)
	assert.Equal(t, int64(6), dbL1Txs[7].EthBlockNum)
	assert.Equal(t, int64(7), dbL1Txs[8].EthBlockNum)
	assert.Equal(t, int64(7), dbL1Txs[9].EthBlockNum)
	assert.Equal(t, int64(7), dbL1Txs[10].EthBlockNum)

	// User Origin
	assert.Equal(t, true, dbL1Txs[0].UserOrigin)
	assert.Equal(t, true, dbL1Txs[1].UserOrigin)
	assert.Equal(t, true, dbL1Txs[2].UserOrigin)
	assert.Equal(t, true, dbL1Txs[3].UserOrigin)
	assert.Equal(t, true, dbL1Txs[4].UserOrigin)
	assert.Equal(t, true, dbL1Txs[5].UserOrigin)

	// Deposit Amount
	assert.Equal(t, big.NewInt(10), dbL1Txs[0].DepositAmount)
	assert.Equal(t, big.NewInt(20), dbL1Txs[1].DepositAmount)
	assert.Equal(t, big.NewInt(0), dbL1Txs[2].DepositAmount)
	assert.Equal(t, big.NewInt(0), dbL1Txs[3].DepositAmount)
	assert.Equal(t, big.NewInt(10), dbL1Txs[4].DepositAmount)
	assert.Equal(t, big.NewInt(0), dbL1Txs[5].DepositAmount)
	assert.Equal(t, big.NewInt(10), dbL1Txs[6].DepositAmount)
	assert.Equal(t, big.NewInt(10), dbL1Txs[7].DepositAmount)
	assert.Equal(t, big.NewInt(0), dbL1Txs[8].DepositAmount)
	assert.Equal(t, big.NewInt(0), dbL1Txs[9].DepositAmount)
	assert.Equal(t, big.NewInt(0), dbL1Txs[10].DepositAmount)

	// Check saved txID's batch_num is not nil
	assert.NotEqual(t, null, dbL1Txs[len(dbL1Txs)-4].BatchNum)


	// // Tx Type
	// assert.Equal(t, common.TxTypeCreateVouch, dbL2Txs[0].Type)
	// assert.Equal(t, common.TxTypeCreateVouch, dbL2Txs[1].Type)
	// assert.Equal(t, common.TxTypeExit, dbL2Txs[2].Type)
	// assert.Equal(t, common.TxTypeCreateVouch, dbL2Txs[3].Type)
	// assert.Equal(t, common.TxTypeCreateVouch, dbL2Txs[4].Type)
	// assert.Equal(t, common.TxTypeDeleteVouch, dbL2Txs[5].Type)

	// // Tx ID
	// assert.Equal(t, "0x0222d2c1f4190752ad0d273024267197c5c65e1069dfff1baca7302e3fbca3c523", dbL2Txs[0].TxID.String())
	// assert.Equal(t, "0x0297e5d629ad9740f54172116adcde0751068486987ffd904c0c46f3df8d9c81fb", dbL2Txs[1].TxID.String())
	// assert.Equal(t, "0x021c11dcf29666db7add866b4969e7e1cbbb9d22debe27709310004cd56d969957", dbL2Txs[2].TxID.String())
	// assert.Equal(t, "0x0217cc446dfb442ed9f0387b01c0e67166a5dbdc86fca8c6fef7df0fa2e2216c4a", dbL2Txs[3].TxID.String())
	// assert.Equal(t, "0x02cbd4e1312607ee5074dd9897b57e84f9d8730f00afdf34a78739a697baecd7d5", dbL2Txs[4].TxID.String())
	// assert.Equal(t, "0x028c138f5d828978c2c578280a70dc09353944312d5597e9972838146e8c539ec6", dbL2Txs[5].TxID.String())

	// // Batch Number
	// assert.Equal(t, common.BatchNum(3), dbL2Txs[0].BatchNum)
	// assert.Equal(t, common.BatchNum(3), dbL2Txs[1].BatchNum)
	// assert.Equal(t, common.BatchNum(4), dbL2Txs[2].BatchNum)
	// assert.Equal(t, common.BatchNum(10), dbL2Txs[3].BatchNum)
	// assert.Equal(t, common.BatchNum(10), dbL2Txs[4].BatchNum)
	// assert.Equal(t, common.BatchNum(10), dbL2Txs[5].BatchNum)

	// // eth_block_num
	// assert.Equal(t, int64(2), dbL2Txs[0].EthBlockNum)
	// assert.Equal(t, int64(2), dbL2Txs[1].EthBlockNum)
	// assert.Equal(t, int64(3), dbL2Txs[2].EthBlockNum)
	// assert.Equal(t, int64(7), dbL2Txs[3].EthBlockNum)
	// assert.Equal(t, int64(7), dbL2Txs[4].EthBlockNum)
	// assert.Equal(t, int64(7), dbL2Txs[5].EthBlockNum)

	// // Amount
	// assert.Equal(t, big.NewInt(10), dbL2Txs[2].Amount)
}

func TestExitTree(t *testing.T) {
	setup(historyDB, t)

	nBatches := 17

	blocks := setTestBlocks(1, 10)
	batches := test.GenBatches(nBatches, blocks)
	err := historyDB.AddBatches(batches)
	assert.NoError(t, err)

	const nAccounts = 3
	accs := test.GenAccounts(nAccounts, 0, nil, nil, batches)
	assert.NoError(t, historyDB.AddAccounts(accs))

	exitTree := test.GenExitTree(nBatches, batches, accs, blocks)
	err = historyDB.AddExitTree(exitTree)
	assert.NoError(t, err)
}

func TestGetUnforgedL1UserTxs(t *testing.T) {
	setup(historyDB, t)

	set := `
		Type: Blockchain

		CreateAccountDeposit A: 20
		CreateAccountDeposit A: 20
		CreateAccountDeposit B: 5
		CreateAccountDeposit C: 5
		CreateAccountDeposit D: 5
		> block

		> batchL1
		> block

		CreateAccountDeposit E: 5
		CreateAccountDeposit F: 5
		> block

	`
	tc := til.NewContext(uint64(0), 128)
	blocks, err := tc.GenerateBlocks(set)
	require.NoError(t, err)
	// Sanity check
	require.Equal(t, 3, len(blocks))
	require.Equal(t, 5, len(blocks[0].Rollup.L1UserTxs))

	for i := range blocks {
		err = historyDB.AddBlockSCData(&blocks[i])
		require.NoError(t, err)
	}

	l1UserTxs, err := historyDB.GetUnforgedL1UserFutureTxs(0)
	require.NoError(t, err)
	assert.Equal(t, 7, len(l1UserTxs))

	l1UserTxs, err = historyDB.GetUnforgedL1UserTxs(1)
	require.NoError(t, err)
	assert.Equal(t, 5, len(l1UserTxs))
	assert.Equal(t, blocks[0].Rollup.L1UserTxs, l1UserTxs)

	l1UserTxs, err = historyDB.GetUnforgedL1UserFutureTxs(1)
	require.NoError(t, err)
	assert.Equal(t, 2, len(l1UserTxs))

	count, err := historyDB.GetUnforgedL1UserTxsCount()
	require.NoError(t, err)
	assert.Equal(t, 7, count)

	l1UserTxs, err = historyDB.GetUnforgedL1UserTxs(2)
	require.NoError(t, err)
	assert.Equal(t, 2, len(l1UserTxs))

	l1UserTxs, err = historyDB.GetUnforgedL1UserFutureTxs(2)
	require.NoError(t, err)
	assert.Equal(t, 0, len(l1UserTxs))

	// No l1UserTxs for this toForgeL1TxsNum
	l1UserTxs, err = historyDB.GetUnforgedL1UserTxs(3)
	require.NoError(t, err)
	assert.Equal(t, 0, len(l1UserTxs))
}

func exampleInitSCVars() *common.RollupVariables { // *common.AuctionVariables,
	// *common.WDelayerVariables,

	rollup := &common.RollupVariables{
		EthBlockNum: 0,
		// FeeAddToken:           big.NewInt(10),
		ForgeL1L2BatchTimeout: 12,
		// WithdrawalDelay:       13,
		Buckets:  []common.BucketParams{},
		SafeMode: false,
	}
	// auction := &common.AuctionVariables{
	// 	EthBlockNum:        0,
	// 	DonationAddress:    ethCommon.BigToAddress(big.NewInt(2)),
	// 	BootCoordinator:    ethCommon.BigToAddress(big.NewInt(3)),
	// 	BootCoordinatorURL: "https://boot.coord.com",
	// 	DefaultSlotSetBid: [6]*big.Int{
	// 		big.NewInt(1), big.NewInt(2), big.NewInt(3),
	// 		big.NewInt(4), big.NewInt(5), big.NewInt(6),
	// 	},
	// 	DefaultSlotSetBidSlotNum: 0,
	// 	ClosedAuctionSlots:       2,
	// 	OpenAuctionSlots:         4320,
	// 	AllocationRatio:          [3]uint16{10, 11, 12},
	// 	Outbidding:               1000,
	// 	SlotDeadline:             20,
	// }
	// wDelayer := &common.WDelayerVariables{
	// 	EthBlockNum:                0,
	// 	HermezGovernanceAddress:    ethCommon.BigToAddress(big.NewInt(2)),
	// 	EmergencyCouncilAddress:    ethCommon.BigToAddress(big.NewInt(3)),
	// 	WithdrawalDelay:            13,
	// 	EmergencyModeStartingBlock: 14,
	// 	EmergencyMode:              false,
	// }
	return rollup //, auction, wDelayer
}

func TestSetInitialSCVars(t *testing.T) {
	setup(historyDB, t)

	_, err := historyDB.GetSCVars()
	assert.Equal(t, sql.ErrNoRows, err)
	rollup := exampleInitSCVars()
	err = historyDB.SetInitialSCVars(rollup)
	require.NoError(t, err)
	dbRollup, err := historyDB.GetSCVars()
	require.NoError(t, err)
	require.Equal(t, rollup, dbRollup)
}

func TestSetExtraInfoForgedL1UserTxs(t *testing.T) {
	setup(historyDB, t)

	set := `
		Type: Blockchain

		CreateAccountDeposit A: 2000
		CreateAccountDeposit B: 500
		CreateAccountDeposit C: 500

		> batchL1 // forge L1UserTxs{nil}, freeze defined L1UserTxs{*}
		> block // blockNum=2

		> batchL1 // forge defined L1UserTxs{*}
		> block // blockNum=3
	`

	tc := til.NewContext(uint64(0), common.RollupConstMaxL1UserTx)
	tilCfgExtra := til.ConfigExtra{
		BootCoordAddr: ethCommon.HexToAddress("0xE39fEc6224708f0772D2A74fd3f9055A90E0A9f2"),
		CoordUser:     "A",
	}
	blocks, err := tc.GenerateBlocks(set)
	require.NoError(t, err)

	err = tc.FillBlocksExtra(blocks, &tilCfgExtra)
	require.NoError(t, err)

	err = tc.FillBlocksForgedL1UserTxs(blocks)
	require.NoError(t, err)

	// Add only first block so that the L1UserTxs are not marked as forged
	for i := range blocks[:1] {
		err = historyDB.AddBlockSCData(&blocks[i])
		require.NoError(t, err)
	}
	// Add second batch to trigger the update of the batch_num,
	// while avoiding the implicit call of setExtraInfoForgedL1UserTxs
	err = historyDB.addBlock(historyDB.dbWrite, &blocks[1].Block)
	require.NoError(t, err)
	err = historyDB.addBatch(historyDB.dbWrite, &blocks[1].Rollup.Batches[0].Batch)
	require.NoError(t, err)
	err = historyDB.addAccounts(historyDB.dbWrite, blocks[1].Rollup.Batches[0].CreatedAccounts)
	require.NoError(t, err)

	// Set the Effective{Amount,DepositAmount} of the L1UserTxs that are forged in the second block
	l1Txs := blocks[1].Rollup.Batches[0].L1UserTxs
	require.Equal(t, 3, len(l1Txs))
	// Change some values to test all cases
	l1Txs[1].EffectiveAmount = big.NewInt(0)
	l1Txs[2].EffectiveDepositAmount = big.NewInt(0)
	l1Txs[2].EffectiveAmount = big.NewInt(0)
	err = historyDB.setExtraInfoForgedL1UserTxs(historyDB.dbWrite, l1Txs)
	require.NoError(t, err)

	dbL1Txs, err := historyDB.GetAllL1UserTxs()
	require.NoError(t, err)
	for i, tx := range dbL1Txs {
		log.Infof("%d %v %v", i, tx.EffectiveAmount, tx.EffectiveDepositAmount)
		assert.NotNil(t, tx.EffectiveAmount)
		assert.NotNil(t, tx.EffectiveDepositAmount)
		switch tx.TxID {
		case l1Txs[0].TxID:
			assert.Equal(t, l1Txs[0].DepositAmount, tx.EffectiveDepositAmount)
			assert.Equal(t, l1Txs[0].Amount, tx.EffectiveAmount)
		case l1Txs[1].TxID:
			assert.Equal(t, l1Txs[1].DepositAmount, tx.EffectiveDepositAmount)
			assert.Equal(t, big.NewInt(0), tx.EffectiveAmount)
		case l1Txs[2].TxID:
			assert.Equal(t, big.NewInt(0), tx.EffectiveDepositAmount)
			assert.Equal(t, big.NewInt(0), tx.EffectiveAmount)
		}
	}
}

// func TestUpdateExitTree(t *testing.T) {
// 	setup(historyDB, t)

// 	set := `
// 		Type: Blockchain

// 		CreateAccountDeposit C: 2000 // Idx=256+2=258
// 		CreateAccountDeposit D: 500  // Idx=256+3=259

// 		// CreateAccountCoordinator A // Idx=256+0=256
// 		// CreateAccountCoordinator B // Idx=256+1=257

// 		> batchL1 // forge L1UserTxs{nil}, freeze defined L1UserTxs{5}
// 		> batchL1 // forge defined L1UserTxs{5}, freeze L1UserTxs{nil}
// 		> block // blockNum=2

// 		// ForceExit A: 100
// 		// ForceExit B: 80

// 		// Exit C: 50
// 		// Exit D: 30

// 		> batchL1 // forge L1UserTxs{nil}, freeze defined L1UserTxs{3}
// 		> batchL1 // forge L1UserTxs{3}, freeze defined L1UserTxs{nil}
// 		> block // blockNum=3

// 		> block // blockNum=4 (empty block)
// 		> block // blockNum=5 (empty block)
// 	`

// 	tc := til.NewContext(uint16(0), common.RollupConstMaxL1UserTx)
// 	tilCfgExtra := til.ConfigExtra{
// 		BootCoordAddr: ethCommon.HexToAddress("0xE39fEc6224708f0772D2A74fd3f9055A90E0A9f2"),
// 		CoordUser:     "A",
// 	}
// 	blocks, err := tc.GenerateBlocks(set)
// 	require.NoError(t, err)
// 	err = tc.FillBlocksExtra(blocks, &tilCfgExtra)
// 	require.NoError(t, err)

// 	// Add all blocks except for the last two
// 	for i := range blocks[:len(blocks)-2] {
// 		err = historyDB.AddBlockSCData(&blocks[i])
// 		require.NoError(t, err)
// 	}

// 	// Add withdraws to the second-to-last block, and insert block into the DB
// 	block := &blocks[len(blocks)-2]
// 	require.Equal(t, int64(4), block.Block.Num)
// 	// tokenAddr := blocks[0].Rollup.AddedTokens[0].EthAddr
// 	// block.WDelayer.Deposits = append(block.WDelayer.Deposits,
// 	// 	common.WDelayerTransfer{Owner: tc.UsersByIdx[257].Addr, Token: tokenAddr, Amount: big.NewInt(80)}, // 257
// 	// 	common.WDelayerTransfer{Owner: tc.UsersByIdx[259].Addr, Token: tokenAddr, Amount: big.NewInt(15)}, // 259
// 	// )
// 	block.Rollup.Withdrawals = append(block.Rollup.Withdrawals,
// 		common.WithdrawInfo{Idx: 256, NumExitRoot: 3, InstantWithdraw: true},
// 		common.WithdrawInfo{Idx: 257, NumExitRoot: 3, InstantWithdraw: true, Owner: tc.AccountsByIdx[257].Addr}, //, Token: tokenAddr},
// 		// common.WithdrawInfo{Idx: 258, NumExitRoot: 3, InstantWithdraw: true},
// 		// common.WithdrawInfo{Idx: 259, NumExitRoot: 3, InstantWithdraw: false, Owner: tc.AccountsByIdx[259].Addr}, //, Token: tokenAddr},
// 	)
// 	err = historyDB.addBlock(historyDB.dbWrite, &block.Block)
// 	require.NoError(t, err)

// 	err = historyDB.updateExitTree(historyDB.dbWrite, block.Block.Num,
// 		block.Rollup.Withdrawals)
// 	require.NoError(t, err)

// 	// Check that exits in DB match with the expected values
// 	dbExits, err := historyDB.GetAllExits()
// 	require.NoError(t, err)
// 	assert.Equal(t, 2, len(dbExits))
// 	dbExitsByIdx := make(map[common.AccountIdx]common.ExitInfo)
// 	for _, dbExit := range dbExits {
// 		dbExitsByIdx[dbExit.AccountIdx] = dbExit
// 	}
// 	for _, withdraw := range block.Rollup.Withdrawals {
// 		assert.Equal(t, withdraw.NumExitRoot, dbExitsByIdx[withdraw.Idx].BatchNum)
// 		assert.Equal(t, &block.Block.Num, dbExitsByIdx[withdraw.Idx].InstantWithdrawn)
// 	}

// 	// Add delayed withdraw to the last block, and insert block into the DB
// 	// block = &blocks[len(blocks)-1]
// 	// require.Equal(t, int64(5), block.Block.Num)
// 	// err = historyDB.addBlock(historyDB.dbWrite, &block.Block)
// 	// require.NoError(t, err)

// 	// err = historyDB.updateExitTree(historyDB.dbWrite, block.Block.Num,
// 	// 	block.Rollup.Withdrawals)
// 	// require.NoError(t, err)

// 	// // Check that delayed withdrawn has been set
// 	// dbExits, err = historyDB.GetAllExits()
// 	// require.NoError(t, err)
// 	// for _, dbExit := range dbExits {
// 	// 	dbExitsByIdx[dbExit.AccountIdx] = dbExit
// 	// }
// 	// require.Equal(t, block.Block.Num, dbExitsByIdx[257].DelayedWithdrawn)
// }

func TestAddBucketUpdates(t *testing.T) {
	setup(historyDB, t)

	const fromBlock int64 = 1
	const toBlock int64 = 5 + 1
	setTestBlocks(fromBlock, toBlock)

	bucketUpdates := []common.BucketUpdate{
		{
			EthBlockNum: 4,
			NumBucket:   0,
			BlockStamp:  4,
			Withdrawals: big.NewInt(123),
		},
		{
			EthBlockNum: 5,
			NumBucket:   2,
			BlockStamp:  5,
			Withdrawals: big.NewInt(42),
		},
	}
	err := historyDB.addBucketUpdates(historyDB.dbWrite, bucketUpdates)
	require.NoError(t, err)
	dbBucketUpdates, err := historyDB.GetAllBucketUpdates()
	require.NoError(t, err)
	assert.Equal(t, bucketUpdates, dbBucketUpdates)
}

func TestGetLastL1TxsNum(t *testing.T) {
	setup(historyDB, t)

	_, err := historyDB.GetLastL1TxsNum()
	assert.NoError(t, err)
}

func TestGetLastTxsPosition(t *testing.T) {
	setup(historyDB, t)

	_, err := historyDB.GetLastTxsPosition(0)
	assert.Equal(t, sql.ErrNoRows.Error(), err.Error())
}

func TestGetFirstBatchBlockNumBySlot(t *testing.T) {
	setup(historyDB, t)

	set := `
		Type: Blockchain

		// Slot = 0

		> block // 2
		> block // 3
		> block // 4
		> block // 5

		// Slot = 1

		> block // 6
		> block // 7
		> batchL1
		> block // 8
		> block // 9

		// Slot = 2

		> batchL1
		> block // 10
		> block // 11
		> block // 12
		> block // 13

	`
	tc := til.NewContext(uint64(0), common.RollupConstMaxL1UserTx)
	blocks, err := tc.GenerateBlocks(set)
	assert.NoError(t, err)

	tilCfgExtra := til.ConfigExtra{
		CoordUser: "A",
	}
	err = tc.FillBlocksExtra(blocks, &tilCfgExtra)
	require.NoError(t, err)

	for i := range blocks {
		for j := range blocks[i].Rollup.Batches {
			blocks[i].Rollup.Batches[j].Batch.SlotNum = int64(i) / 4
		}
	}

	// Add all blocks
	for i := range blocks {
		err = historyDB.AddBlockSCData(&blocks[i])
		require.NoError(t, err)
	}

	_, err = historyDB.GetFirstBatchBlockNumBySlot(0)
	require.Equal(t, sql.ErrNoRows, common.Unwrap(err))

	bn1, err := historyDB.GetFirstBatchBlockNumBySlot(1)
	require.NoError(t, err)
	assert.Equal(t, int64(8), bn1)

	bn2, err := historyDB.GetFirstBatchBlockNumBySlot(2)
	require.NoError(t, err)
	assert.Equal(t, int64(10), bn2)
}

func TestTxItemID(t *testing.T) {
	setup(historyDB, t)

	testUsersLen := 10
	var set []til.Instruction
	for user := 0; user < testUsersLen; user++ {
		set = append(set, til.Instruction{
			Typ: common.TxTypeCreateAccountDeposit,
			// TokenID:       common.TokenID(0),
			DepositAmount: big.NewInt(1000000),
			Amount:        big.NewInt(0),
			From:          fmt.Sprintf("User%02d", user),
		})
		set = append(set, til.Instruction{Typ: til.TypeNewBlock})
	}
	set = append(set, til.Instruction{Typ: til.TypeNewBatchL1})
	set = append(set, til.Instruction{Typ: til.TypeNewBatchL1})
	set = append(set, til.Instruction{Typ: til.TypeNewBlock}) // block 11

	for user := 0; user < testUsersLen; user++ {
		set = append(set, til.Instruction{
			Typ: common.TxTypeDeposit,
			// TokenID:       common.TokenID(0),
			DepositAmount: big.NewInt(100000),
			Amount:        big.NewInt(0),
			From:          fmt.Sprintf("User%02d", user),
		})
		set = append(set, til.Instruction{Typ: til.TypeNewBlock})
	}
	set = append(set, til.Instruction{Typ: til.TypeNewBatchL1})
	set = append(set, til.Instruction{Typ: til.TypeNewBatchL1})
	set = append(set, til.Instruction{Typ: til.TypeNewBlock}) // block 22

	for user := 0; user < testUsersLen; user++ {
		set = append(set, til.Instruction{
			Typ: common.TxTypeForceExit,
			// TokenID:       common.TokenID(0),
			Amount:        big.NewInt(10 * int64(user+1)),
			DepositAmount: big.NewInt(0),
			From:          fmt.Sprintf("User%02d", user),
		})
		set = append(set, til.Instruction{Typ: til.TypeNewBlock})
	}
	set = append(set, til.Instruction{Typ: til.TypeNewBatchL1})
	set = append(set, til.Instruction{Typ: til.TypeNewBatchL1})
	set = append(set, til.Instruction{Typ: til.TypeNewBlock}) // block 33

	var chainID uint64 = 0
	tc := til.NewContext(chainID, common.RollupConstMaxL1UserTx)
	blocks, err := tc.GenerateBlocksFromInstructions(set)
	assert.NoError(t, err)

	tilCfgExtra := til.ConfigExtra{
		// CoordUser: "A",
	}
	err = tc.FillBlocksExtra(blocks, &tilCfgExtra)
	require.NoError(t, err)

	// Add all blocks
	for i := range blocks {
		err = historyDB.AddBlockSCData(&blocks[i])
		require.NoError(t, err)
	}

	txs, err := historyDB.GetAllL1UserTxs()
	require.NoError(t, err)
	position := 0
	for _, tx := range txs {
		if tx.Position == 0 {
			position = 0
		}
		assert.Equal(t, position, tx.Position)
		position++
	}
}

func assertEqualBlock(t *testing.T, expected *common.Block, actual *common.Block) {
	assert.Equal(t, expected.Num, actual.Num)
	assert.Equal(t, expected.Hash, actual.Hash)
	assert.Equal(t, expected.Timestamp.Unix(), actual.Timestamp.Unix())
}

// setTestBlocks WARNING: this will delete the blocks and recreate them
func setTestBlocks(from, to int64) []common.Block {
	test.WipeDB(historyDB.DB())
	blocks := test.GenBlocks(from, to)
	if err := historyDB.AddBlocks(blocks); err != nil {
		panic(err)
	}
	return blocks
}
