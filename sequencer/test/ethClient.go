package test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"sync"
	"time"

	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/eth"
	"tokamak-sybil-resistance/log"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethKeystore "github.com/ethereum/go-ethereum/accounts/keystore"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/mitchellh/copystructure"
)

func init() {
	log.Init("debug", []string{"stdout"})
	copystructure.Copiers[reflect.TypeOf(big.Int{})] =
		func(raw interface{}) (interface{}, error) {
			in := raw.(big.Int)
			out := new(big.Int).Set(&in)
			return *out, nil
		}
}

// RollupBlock stores all the data related to the Rollup SC from an ethereum block
type RollupBlock struct {
	State     eth.RollupState
	Vars      common.RollupVariables
	Events    eth.RollupEvents
	Txs       map[ethCommon.Hash]*types.Transaction
	Constants *common.RollupConstants
	Eth       *EthereumBlock
}

func (r *RollupBlock) addTransaction(tx *types.Transaction) *types.Transaction {
	txHash := tx.Hash()
	r.Txs[txHash] = tx
	return tx
}

// var (
// 	errBidClosed   = fmt.Errorf("bid has already been closed")
// 	errBidNotOpen  = fmt.Errorf("bid has not been opened yet")
// 	errBidBelowMin = fmt.Errorf("bid below minimum")
// 	errCoordNotReg = fmt.Errorf("coordinator not registered")
// )

// EthereumBlock stores all the generic data related to the an ethereum block
type EthereumBlock struct {
	BlockNum   int64
	Time       int64
	Hash       ethCommon.Hash
	ParentHash ethCommon.Hash
	Tokens     map[ethCommon.Address]eth.ERC20Consts
	Nonce      uint64
	// state      ethState
}

// Block represents a ethereum block
type Block struct {
	Rollup *RollupBlock
	Eth    *EthereumBlock
}

func (b *Block) copy() *Block {
	bCopyRaw, err := copystructure.Copy(b)
	if err != nil {
		panic(err)
	}
	bCopy := bCopyRaw.(*Block)
	return bCopy
}

// Next prepares the successive block.
func (b *Block) Next() *Block {
	blockNext := b.copy()
	blockNext.Rollup.Events = eth.NewRollupEvents()

	blockNext.Eth.BlockNum = b.Eth.BlockNum + 1
	blockNext.Eth.ParentHash = b.Eth.Hash

	blockNext.Rollup.Constants = b.Rollup.Constants
	blockNext.Rollup.Eth = blockNext.Eth

	return blockNext
}

// ClientSetup is used to initialize the constants of the Smart Contracts and
// other details of the test Client
type ClientSetup struct {
	RollupConstants *common.RollupConstants
	RollupVariables *common.RollupVariables
	VerifyProof     bool
	ChainID         *big.Int
}

// NewClientSetupExample returns a ClientSetup example with hardcoded realistic
// values.  With this setup, the rollup genesis will be block 1, and block 0
// and 1 will be premined.
//
//nolint:gomnd
func NewClientSetupExample() *ClientSetup {
	governanceAddress := ethCommon.HexToAddress("0x688EfD95BA4391f93717CF02A9aED9DBD2855cDd")
	rollupConstants := &common.RollupConstants{
		Verifiers: []common.RollupVerifierStruct{
			{
				MaxTx:   2048,
				NLevels: 32,
			},
		},
		TokamakGovernanceAddress: governanceAddress,
	}
	rollupVariables := &common.RollupVariables{
		ForgeL1L2BatchTimeout: 10,
		Buckets:               []common.BucketParams{},
	}
	return &ClientSetup{
		RollupConstants: rollupConstants,
		RollupVariables: rollupVariables,
		VerifyProof:     false,
		ChainID:         big.NewInt(0),
	}
}

// Timer is an interface to simulate a source of time, useful to advance time
// virtually.
type Timer interface {
	Time() int64
}

// type forgeBatchArgs struct {
// 	ethTx     *types.Transaction
// 	blockNum  int64
// 	blockHash ethCommon.Hash
// }

type batch struct {
	ForgeBatchArgs eth.RollupForgeBatchArgs
	Sender         ethCommon.Address
}

// Client implements the eth.ClientInterface interface, allowing to manipulate the
// values for testing, working with deterministic results.
type Client struct {
	rw              *sync.RWMutex
	log             bool
	addr            *ethCommon.Address
	chainID         *big.Int
	rollupConstants *common.RollupConstants
	blocks          map[int64]*Block
	// state            state
	blockNum    int64 // last mined block num
	maxBlockNum int64 // highest block num calculated
	timer       Timer
	hasher      hasher

	forgeBatchArgsPending map[ethCommon.Hash]*batch
	forgeBatchArgs        map[ethCommon.Hash]*batch

	startBlock int64
}

// NewClient returns a new test Client that implements the eth.IClient
// interface, at the given initialBlockNumber.
func NewClient(l bool, timer Timer, addr *ethCommon.Address, setup *ClientSetup) *Client {
	blocks := make(map[int64]*Block)
	blockNum := int64(0)

	hasher := hasher{}
	// Add ethereum genesis block
	mapL1TxQueue := make(map[int64]*eth.QueueStruct)
	mapL1TxQueue[0] = eth.NewQueueStruct()
	mapL1TxQueue[1] = eth.NewQueueStruct()
	blockCurrent := &Block{
		Rollup: &RollupBlock{
			State: eth.RollupState{
				AccountRoot:            big.NewInt(0),
				VouchRoot:              big.NewInt(0),
				ScoreRoot:              big.NewInt(0),
				ExitRoots:              make([]*big.Int, 1),
				ExitNullifierMap:       make(map[int64]map[int64]bool),
				MapL1TxQueue:           mapL1TxQueue,
				LastL1L2Batch:          0,
				CurrentToForgeL1TxsNum: 0,
				LastToForgeL1TxsNum:    1,
				CurrentIdx:             0,
			},
			Vars:      *setup.RollupVariables,
			Txs:       make(map[ethCommon.Hash]*types.Transaction),
			Events:    eth.NewRollupEvents(),
			Constants: setup.RollupConstants,
		},
		Eth: &EthereumBlock{
			BlockNum:   blockNum,
			Time:       timer.Time(),
			Hash:       hasher.Next(),
			ParentHash: ethCommon.Hash{},
			Tokens:     make(map[ethCommon.Address]eth.ERC20Consts),
		},
	}
	blockCurrent.Rollup.Eth = blockCurrent.Eth
	blocks[blockNum] = blockCurrent
	blockNext := blockCurrent.Next()
	blocks[blockNum+1] = blockNext

	c := Client{
		rw:                    &sync.RWMutex{},
		log:                   l,
		addr:                  addr,
		rollupConstants:       setup.RollupConstants,
		blocks:                blocks,
		timer:                 timer,
		hasher:                hasher,
		forgeBatchArgsPending: make(map[ethCommon.Hash]*batch),
		forgeBatchArgs:        make(map[ethCommon.Hash]*batch),
		blockNum:              blockNum,
		maxBlockNum:           blockNum,
	}

	if c.startBlock == 0 {
		c.startBlock = 2
	}
	for i := int64(1); i < c.startBlock; i++ {
		c.CtlMineBlock()
	}

	return &c
}

//
// Mock Control
//

func (c *Client) setNextBlock(block *Block) {
	c.blocks[c.blockNum+1] = block
}

func (c *Client) revertIfErr(err error, block *Block) {
	if err != nil {
		log.Infow("TestClient revert", "block", block.Eth.BlockNum, "err", err)
		c.setNextBlock(block)
	}
}

// Debugf calls log.Debugf if c.log is true
func (c *Client) Debugf(template string, args ...interface{}) {
	if c.log {
		log.Debugf(template, args...)
	}
}

// Debugw calls log.Debugw if c.log is true
func (c *Client) Debugw(template string, kv ...interface{}) {
	if c.log {
		log.Debugw(template, kv...)
	}
}

type hasher struct {
	counter uint64
}

// Next returns the next hash
func (h *hasher) Next() ethCommon.Hash {
	var hash ethCommon.Hash
	binary.LittleEndian.PutUint64(hash[:], h.counter)
	h.counter++
	return hash
}

func (c *Client) nextBlock() *Block {
	return c.blocks[c.blockNum+1]
}

func (c *Client) currentBlock() *Block {
	return c.blocks[c.blockNum]
}

// CtlSetAddr sets the address of the client
func (c *Client) CtlSetAddr(addr ethCommon.Address) {
	c.addr = &addr
}

// CtlMineBlock moves one block forward
func (c *Client) CtlMineBlock() {
	c.rw.Lock()
	defer c.rw.Unlock()

	blockCurrent := c.nextBlock()
	c.blockNum++
	c.maxBlockNum = c.blockNum
	blockCurrent.Eth.Time = c.timer.Time()
	blockCurrent.Eth.Hash = c.hasher.Next()
	for ethTxHash, forgeBatchArgs := range c.forgeBatchArgsPending {
		c.forgeBatchArgs[ethTxHash] = forgeBatchArgs
	}
	c.forgeBatchArgsPending = make(map[ethCommon.Hash]*batch)

	blockNext := blockCurrent.Next()
	c.blocks[c.blockNum+1] = blockNext
	c.Debugw("TestClient mined block", "blockNum", c.blockNum)
}

// CtlRollback discards the last mined block.  Use this to replace a mined
// block to simulate reorgs.
func (c *Client) CtlRollback() {
	c.rw.Lock()
	defer c.rw.Unlock()

	if c.blockNum == 0 {
		panic("Can't rollback at blockNum = 0")
	}
	delete(c.blocks, c.blockNum+1) // delete next block
	delete(c.blocks, c.blockNum)   // delete current block
	c.blockNum--
	blockCurrent := c.blocks[c.blockNum]
	blockNext := blockCurrent.Next()
	c.blocks[c.blockNum+1] = blockNext
}

//
// Ethereum
//

// CtlLastBlock returns the last blockNum without checks
func (c *Client) CtlLastBlock() *common.Block {
	c.rw.RLock()
	defer c.rw.RUnlock()

	block := c.blocks[c.blockNum]
	return &common.Block{
		Num:        c.blockNum,
		Timestamp:  time.Unix(block.Eth.Time, 0),
		Hash:       block.Eth.Hash,
		ParentHash: block.Eth.ParentHash,
	}
}

// CtlLastForgedBatch returns the last batchNum without checks
func (c *Client) CtlLastForgedBatch() int64 {
	c.rw.RLock()
	defer c.rw.RUnlock()

	currentBlock := c.currentBlock()
	e := currentBlock.Rollup
	return int64(len(e.State.ExitRoots)) - 1
}

// EthChainID returns the ChainID of the ethereum network
func (c *Client) EthChainID() (*big.Int, error) {
	return c.chainID, nil
}

// EthPendingNonceAt returns the account nonce of the given account in the pending
// state. This is the nonce that should be used for the next transaction.
func (c *Client) EthPendingNonceAt(ctx context.Context, account ethCommon.Address) (uint64, error) {
	// NOTE: For now Client doesn't simulate nonces
	return 0, nil
}

// EthNonceAt returns the account nonce of the given account. The block number can
// be nil, in which case the nonce is taken from the latest known block.
func (c *Client) EthNonceAt(ctx context.Context, account ethCommon.Address,
	blockNumber *big.Int) (uint64, error) {
	// NOTE: For now Client doesn't simulate nonces
	return 0, nil
}

// EthSuggestGasPrice retrieves the currently suggested gas price to allow a
// timely execution of a transaction.
func (c *Client) EthSuggestGasPrice(ctx context.Context) (*big.Int, error) {
	// NOTE: For now Client doesn't simulate gasPrice
	return big.NewInt(0), nil
}

// EthKeyStore returns the keystore in the Client
func (c *Client) EthKeyStore() *ethKeystore.KeyStore {
	return nil
}

// EthCall runs the transaction as a call (without paying) in the local node at
// blockNum.
func (c *Client) EthCall(ctx context.Context, tx *types.Transaction,
	blockNum *big.Int) ([]byte, error) {
	return nil, common.Wrap(common.ErrTODO)
}

// EthLastBlock returns the last blockNum
func (c *Client) EthLastBlock() (int64, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	if c.blockNum < c.maxBlockNum {
		panic("blockNum has decreased.  " +
			"After a rollback you must mine to reach the same or higher blockNum")
	}
	return c.blockNum, nil
}

// EthTransactionReceipt returns the transaction receipt of the given txHash
func (c *Client) EthTransactionReceipt(ctx context.Context,
	txHash ethCommon.Hash) (*types.Receipt, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	for i := int64(0); i < c.blockNum; i++ {
		b := c.blocks[i]
		_, ok := b.Rollup.Txs[txHash]
		if ok {
			return &types.Receipt{
				TxHash:      txHash,
				Status:      types.ReceiptStatusSuccessful,
				BlockHash:   b.Eth.Hash,
				BlockNumber: big.NewInt(b.Eth.BlockNum),
			}, nil
		}
	}

	return nil, nil
}

// CtlAddERC20 adds an ERC20 token to the blockchain.
func (c *Client) CtlAddERC20(tokenAddr ethCommon.Address, constants eth.ERC20Consts) {
	nextBlock := c.nextBlock()
	e := nextBlock.Eth
	e.Tokens[tokenAddr] = constants
}

// EthERC20Consts returns the constants defined for a particular ERC20 Token instance.
func (c *Client) EthERC20Consts(tokenAddr ethCommon.Address) (*eth.ERC20Consts, error) {
	currentBlock := c.currentBlock()
	e := currentBlock.Eth
	if constants, ok := e.Tokens[tokenAddr]; ok {
		return &constants, nil
	}
	return nil, common.Wrap(fmt.Errorf("tokenAddr not found"))
}

// func newHeader(number *big.Int) *types.Header {
// 	return &types.Header{
// 		Number: number,
// 		Time:   uint64(number.Int64()),
// 	}
// }

// EthHeaderByNumber returns the *types.Header for the given block number in a
// deterministic way.
// func (c *Client) EthHeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
// 	return newHeader(number), nil
// }

// EthBlockByNumber returns the *common.Block for the given block number in a
// deterministic way.  If number == -1, the latests known block is returned.
func (c *Client) EthBlockByNumber(ctx context.Context, blockNum int64) (*common.Block, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	if blockNum > c.blockNum {
		return nil, ethereum.NotFound
	}
	if blockNum == -1 {
		blockNum = c.blockNum
	}
	block := c.blocks[blockNum]
	return &common.Block{
		Num:        blockNum,
		Timestamp:  time.Unix(block.Eth.Time, 0),
		Hash:       block.Eth.Hash,
		ParentHash: block.Eth.ParentHash,
	}, nil
}

// EthAddress returns the ethereum address of the account loaded into the Client
func (c *Client) EthAddress() (*ethCommon.Address, error) {
	if c.addr == nil {
		return nil, common.Wrap(eth.ErrAccountNil)
	}
	return c.addr, nil
}

var errTODO = fmt.Errorf("TODO: Not implemented yet")

//
// Rollup
//

// CtlAddL1TxUser adds an L1TxUser to the L1UserTxs queue of the Rollup
// func (c *Client) CtlAddL1TxUser(l1Tx *common.L1Tx) {
// 	c.rw.Lock()
// 	defer c.rw.Unlock()
//
// 	nextBlock := c.nextBlock()
// 	r := nextBlock.Rollup
// 	queue := r.State.MapL1TxQueue[r.State.LastToForgeL1TxsNum]
// 	if len(queue.L1TxQueue) >= eth.RollupConstMaxL1UserTx {
// 		r.State.LastToForgeL1TxsNum++
// 		r.State.MapL1TxQueue[r.State.LastToForgeL1TxsNum] = eth.NewQueueStruct()
// 		queue = r.State.MapL1TxQueue[r.State.LastToForgeL1TxsNum]
// 	}
// 	if int64(l1Tx.FromIdx) > r.State.CurrentIdx {
// 		panic("l1Tx.FromIdx > r.State.CurrentIdx")
// 	}
// 	if int(l1Tx.TokenID)+1 > len(r.State.TokenList) {
// 		panic("l1Tx.TokenID + 1 > len(r.State.TokenList)")
// 	}
// 	queue.L1TxQueue = append(queue.L1TxQueue, *l1Tx)
// 	r.Events.L1UserTx = append(r.Events.L1UserTx, eth.RollupEventL1UserTx{
// 		L1Tx:            *l1Tx,
// 		ToForgeL1TxsNum: r.State.LastToForgeL1TxsNum,
// 		Position:        len(queue.L1TxQueue) - 1,
// 	})
// }

// RollupL1UserTxERC20Permit is the interface to call the smart contract function
func (c *Client) RollupL1UserTxERC20Permit(fromBJJ babyjub.PublicKeyComp, fromIdx int64,
	depositAmount *big.Int, amount *big.Int, toIdx int64,
	deadline *big.Int) (tx *types.Transaction, err error) {
	log.Error("TODO")
	return nil, common.Wrap(errTODO)
}

// RollupL1UserTxERC20ETH sends an L1UserTx to the Rollup.
func (c *Client) RollupL1UserTxERC20ETH(
	fromBJJ babyjub.PublicKeyComp,
	fromIdx int64,
	depositAmount *big.Int,
	amount *big.Int,
	toIdx int64,
	txType common.TxType,
) (tx *types.Transaction, err error) {
	c.rw.Lock()
	defer c.rw.Unlock()
	cpy := c.nextBlock().copy()
	defer func() { c.revertIfErr(err, cpy) }()

	_, err = common.NewFloat40(amount)
	if err != nil {
		return nil, common.Wrap(err)
	}
	_, err = common.NewFloat40(depositAmount)
	if err != nil {
		return nil, common.Wrap(err)
	}

	nextBlock := c.nextBlock()
	r := nextBlock.Rollup
	queue := r.State.MapL1TxQueue[r.State.LastToForgeL1TxsNum]
	if len(queue.L1TxQueue) >= common.RollupConstMaxL1UserTx {
		r.State.LastToForgeL1TxsNum++
		r.State.MapL1TxQueue[r.State.LastToForgeL1TxsNum] = eth.NewQueueStruct()
		queue = r.State.MapL1TxQueue[r.State.LastToForgeL1TxsNum]
	}
	if fromIdx > r.State.CurrentIdx {
		panic("l1Tx.FromIdx > r.State.CurrentIdx")
	}
	toForgeL1TxsNum := r.State.LastToForgeL1TxsNum
	l1Tx, err := common.NewL1Tx(&common.L1Tx{
		FromIdx:         common.AccountIdx(fromIdx),
		FromEthAddr:     *c.addr,
		FromBJJ:         fromBJJ,
		Amount:          amount,
		DepositAmount:   depositAmount,
		ToIdx:           common.AccountIdx(toIdx),
		ToForgeL1TxsNum: &toForgeL1TxsNum,
		Position:        len(queue.L1TxQueue),
		UserOrigin:      true,
		Type:            txType,
	})
	if err != nil {
		return nil, common.Wrap(err)
	}

	queue.L1TxQueue = append(queue.L1TxQueue, *l1Tx)
	r.Events.L1UserTx = append(r.Events.L1UserTx, eth.RollupEventL1UserTx{
		L1UserTx: *l1Tx,
	})
	return r.addTransaction(c.newTransaction("l1UserTxERC20ETH", l1Tx)), nil
}

// RollupL1UserTxERC777 is the interface to call the smart contract function
// func (c *Client) RollupL1UserTxERC777(fromBJJ *babyjub.PublicKey, fromIdx int64,
// 	depositAmount *big.Int, amount *big.Int, tokenID uint32,
//	toIdx int64) (*types.Transaction, error) {
// 	log.Error("TODO")
// 	return nil, errTODO
// }

// RollupRegisterTokensCount is the interface to call the smart contract function
func (c *Client) RollupRegisterTokensCount() (*big.Int, error) {
	log.Error("TODO")
	return nil, common.Wrap(errTODO)
}

// RollupLastForgedBatch is the interface to call the smart contract function
func (c *Client) RollupLastForgedBatch() (int64, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	currentBlock := c.currentBlock()
	e := currentBlock.Rollup
	return int64(len(e.State.ExitRoots)) - 1, nil
}

// RollupWithdrawCircuit is the interface to call the smart contract function
func (c *Client) RollupWithdrawCircuit(proofA, proofC [2]*big.Int, proofB [2][2]*big.Int,
	numExitRoot, idx int64, amount *big.Int,
	instantWithdraw bool) (*types.Transaction, error) {
	log.Error("TODO")
	return nil, common.Wrap(errTODO)
}

// RollupWithdrawMerkleProof is the interface to call the smart contract function
func (c *Client) RollupWithdrawMerkleProof(babyPubKey babyjub.PublicKeyComp,
	numExitRoot, idx int64, amount *big.Int, siblings []*big.Int,
	instantWithdraw bool) (tx *types.Transaction, err error) {
	c.rw.Lock()
	defer c.rw.Unlock()
	cpy := c.nextBlock().copy()
	defer func() { c.revertIfErr(err, cpy) }()

	nextBlock := c.nextBlock()
	r := nextBlock.Rollup

	if int(numExitRoot) >= len(r.State.ExitRoots) {
		return nil, common.Wrap(fmt.Errorf("numExitRoot >= len(r.State.ExitRoots)"))
	}
	if _, ok := r.State.ExitNullifierMap[numExitRoot][idx]; ok {
		return nil, common.Wrap(fmt.Errorf("exit already withdrawn"))
	}
	r.State.ExitNullifierMap[numExitRoot][idx] = true

	babyPubKeyDecomp, err := babyPubKey.Decompress()
	if err != nil {
		return nil, common.Wrap(err)
	}

	type data struct {
		BabyPubKey      *babyjub.PublicKey
		NumExitRoot     int64
		Idx             int64
		Amount          *big.Int
		Siblings        []*big.Int
		InstantWithdraw bool
	}
	tx = r.addTransaction(c.newTransaction("withdrawMerkleProof", data{
		BabyPubKey:      babyPubKeyDecomp,
		NumExitRoot:     numExitRoot,
		Idx:             idx,
		Amount:          amount,
		Siblings:        siblings,
		InstantWithdraw: instantWithdraw,
	}))
	r.Events.Withdraw = append(r.Events.Withdraw, eth.RollupEventWithdraw{
		Idx:             uint64(idx),
		NumExitRoot:     uint64(numExitRoot),
		InstantWithdraw: instantWithdraw,
		TxHash:          tx.Hash(),
	})

	return tx, nil
}

type transactionData struct {
	Name  string
	Value interface{}
}

func (c *Client) newTransaction(name string, value interface{}) *types.Transaction {
	eth := c.nextBlock().Eth
	nonce := eth.Nonce
	eth.Nonce++
	data, err := json.Marshal(transactionData{name, value})
	if err != nil {
		panic(err)
	}
	return types.NewTransaction(nonce, ethCommon.Address{}, nil, 0, nil,
		data)
}

// RollupForgeBatch is the interface to call the smart contract function
func (c *Client) RollupForgeBatch(args *eth.RollupForgeBatchArgs,
	auth *bind.TransactOpts) (tx *types.Transaction, err error) {
	c.rw.Lock()
	defer c.rw.Unlock()
	cpy := c.nextBlock().copy()
	defer func() { c.revertIfErr(err, cpy) }()
	if c.addr == nil {
		return nil, common.Wrap(eth.ErrAccountNil)
	}

	return c.addBatch(args)
}

// CtlAddBatch adds forged batch to the Rollup, without checking any ZKProof
func (c *Client) CtlAddBatch(args *eth.RollupForgeBatchArgs) {
	c.rw.Lock()
	defer c.rw.Unlock()

	if _, err := c.addBatch(args); err != nil {
		panic(err)
	}
}

func (c *Client) addBatch(args *eth.RollupForgeBatchArgs) (*types.Transaction, error) {
	nextBlock := c.nextBlock()
	r := nextBlock.Rollup
	r.State.AccountRoot = args.NewAccountRoot
	r.State.VouchRoot = args.NewVouchRoot
	r.State.ScoreRoot = args.NewScoreRoot
	if args.NewLastIdx < r.State.CurrentIdx {
		return nil, common.Wrap(fmt.Errorf("args.NewLastIdx < r.State.CurrentIdx"))
	}
	r.State.CurrentIdx = args.NewLastIdx
	r.State.ExitNullifierMap[int64(len(r.State.ExitRoots))] = make(map[int64]bool)
	r.State.ExitRoots = append(r.State.ExitRoots, args.NewExitRoot)
	if args.L1Batch {
		r.State.CurrentToForgeL1TxsNum++
		if r.State.CurrentToForgeL1TxsNum == r.State.LastToForgeL1TxsNum {
			r.State.LastToForgeL1TxsNum++
			r.State.MapL1TxQueue[r.State.LastToForgeL1TxsNum] = eth.NewQueueStruct()
		}
	}
	ethTx := r.addTransaction(c.newTransaction("forgebatch", args))
	c.forgeBatchArgsPending[ethTx.Hash()] = &batch{*args, *c.addr}
	r.Events.ForgeBatch = append(r.Events.ForgeBatch, eth.RollupEventForgeBatch{
		BatchNum:     int64(len(r.State.ExitRoots)) - 1,
		EthTxHash:    ethTx.Hash(),
		L1UserTxsLen: uint16(len(args.L1UserTxs)),
	})

	return ethTx, nil
}

// RollupGetCurrentTokens is the interface to call the smart contract function
func (c *Client) RollupGetCurrentTokens() (*big.Int, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	log.Error("TODO")
	return nil, common.Wrap(errTODO)
}

// RollupUpdateForgeL1L2BatchTimeout is the interface to call the smart contract function
func (c *Client) RollupUpdateForgeL1L2BatchTimeout(newForgeL1Timeout int64) (tx *types.Transaction,
	err error) {
	c.rw.Lock()
	defer c.rw.Unlock()
	cpy := c.nextBlock().copy()
	defer func() { c.revertIfErr(err, cpy) }()
	if c.addr == nil {
		return nil, common.Wrap(eth.ErrAccountNil)
	}

	nextBlock := c.nextBlock()
	r := nextBlock.Rollup
	r.Vars.ForgeL1L2BatchTimeout = newForgeL1Timeout
	r.Events.UpdateForgeL1L2BatchTimeout = append(r.Events.UpdateForgeL1L2BatchTimeout,
		eth.RollupEventUpdateForgeL1L2BatchTimeout{NewForgeL1L2BatchTimeout: newForgeL1Timeout})

	return r.addTransaction(c.newTransaction("updateForgeL1L2BatchTimeout", newForgeL1Timeout)), nil
}

// RollupUpdateFeeAddToken is the interface to call the smart contract function
func (c *Client) RollupUpdateFeeAddToken(newFeeAddToken *big.Int) (tx *types.Transaction,
	err error) {
	c.rw.Lock()
	defer c.rw.Unlock()
	cpy := c.nextBlock().copy()
	defer func() { c.revertIfErr(err, cpy) }()
	if c.addr == nil {
		return nil, common.Wrap(eth.ErrAccountNil)
	}

	log.Error("TODO")
	return nil, common.Wrap(errTODO)
}

// RollupConstants returns the Constants of the Rollup Smart Contract
func (c *Client) RollupConstants() (*common.RollupConstants, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	return c.rollupConstants, nil
}

// RollupEventsByBlock returns the events in a block that happened in the Rollup Smart Contract
func (c *Client) RollupEventsByBlock(blockNum int64,
	blockHash *ethCommon.Hash) (*eth.RollupEvents, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	block, ok := c.blocks[blockNum]
	if !ok {
		return nil, common.Wrap(fmt.Errorf("Block %v doesn't exist", blockNum))
	}
	if blockHash != nil && *blockHash != block.Eth.Hash {
		return nil, common.Wrap(fmt.Errorf("hash mismatch, requested %v got %v",
			blockHash, block.Eth.Hash))
	}
	return &block.Rollup.Events, nil
}

// RollupEventInit returns the initialize event with its corresponding block number
func (c *Client) RollupEventInit(genesisBlockNum int64) (*eth.RollupEventInitialize, int64, error) {
	vars := c.blocks[0].Rollup.Vars
	return &eth.RollupEventInitialize{
		ForgeL1L2BatchTimeout: uint8(vars.ForgeL1L2BatchTimeout),
	}, 1, nil
}

// RollupForgeBatchArgs returns the arguments used in a ForgeBatch call in the Rollup Smart Contract
// in the given transaction
func (c *Client) RollupForgeBatchArgs(ethTxHash ethCommon.Hash,
	l1UserTxsLen uint16) (*eth.RollupForgeBatchArgs, *ethCommon.Address, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()

	batch, ok := c.forgeBatchArgs[ethTxHash]
	if !ok {
		return nil, nil, common.Wrap(fmt.Errorf("transaction not found"))
	}
	return &batch.ForgeBatchArgs, &batch.Sender, nil
}

// CtlAddBlocks adds block data to the smarts contracts.  The added blocks will
// appear as mined.  Not thread safe.
func (c *Client) CtlAddBlocks(blocks []common.BlockData) (err error) {
	// NOTE: We don't lock because internally we call public functions that
	// lock already.
	for _, block := range blocks {
		for _, tx := range block.Rollup.L1UserTxs {
			c.CtlSetAddr(tx.FromEthAddr)
			if _, err := c.RollupL1UserTxERC20ETH(tx.FromBJJ, int64(tx.FromIdx),
				tx.DepositAmount, tx.Amount, int64(tx.ToIdx), tx.Type); err != nil {
				return common.Wrap(err)
			}
		}
		c.CtlSetAddr(ethCommon.HexToAddress("0xE39fEc6224708f0772D2A74fd3f9055A90E0A9f2"))
		for _, batch := range block.Rollup.Batches {
			// auths := make([][]byte, len(batch.L1CoordinatorTxs))
			// for i := range auths {
			// 	auths[i] = make([]byte, 65)
			// }
			if _, err := c.RollupForgeBatch(&eth.RollupForgeBatchArgs{
				NewLastIdx: batch.Batch.LastIdx,

				NewAccountRoot: batch.Batch.AccountRoot,
				NewVouchRoot:   batch.Batch.VouchRoot,
				NewScoreRoot:   batch.Batch.ScoreRoot,
				NewExitRoot:    batch.Batch.ExitRoot,
				// L1CoordinatorTxs:      batch.L1CoordinatorTxs,
				// L1CoordinatorTxsAuths: auths,
				// L2TxsData:             batch.L2Txs,
				// Circuit selector
				VerifierIdx: 0, // Intentionally empty
				L1Batch:     batch.L1Batch,
				ProofA:      [2]*big.Int{},    // Intentionally empty
				ProofB:      [2][2]*big.Int{}, // Intentionally empty
				ProofC:      [2]*big.Int{},    // Intentionally empty
			}, nil); err != nil {
				return common.Wrap(err)
			}
		}
		// Mine block and sync
		c.CtlMineBlock()
	}
	return nil
}
