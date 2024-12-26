package eth

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"tokamak-sybil-resistance/common"
	Sybil "tokamak-sybil-resistance/eth/contracts"
	"tokamak-sybil-resistance/log"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// QueueStruct is the queue of L1Txs for a batch
type QueueStruct struct {
	L1TxQueue    []common.L1Tx
	TotalL1TxFee *big.Int
}

// NewQueueStruct creates a new clear QueueStruct.
func NewQueueStruct() *QueueStruct {
	return &QueueStruct{
		L1TxQueue:    make([]common.L1Tx, 0),
		TotalL1TxFee: big.NewInt(0),
	}
}

// RollupState represents the state of the Rollup in the Smart Contract
type RollupState struct {
	AccountRoot *big.Int
	VouchRoot   *big.Int
	ScoreRoot   *big.Int
	ExitRoots   []*big.Int
	// ExitNullifierMap       map[[256 / 8]byte]bool
	ExitNullifierMap       map[int64]map[int64]bool // batchNum -> idx -> bool
	MapL1TxQueue           map[int64]*QueueStruct
	LastL1L2Batch          int64
	CurrentToForgeL1TxsNum int64
	LastToForgeL1TxsNum    int64
	CurrentIdx             int64
}

// RollupEventInitialize is the InitializeHermezEvent event of the
// Smart Contract
type RollupEventInitialize struct {
	ForgeL1L2BatchTimeout uint8
	FeeAddToken           *big.Int
	WithdrawalDelay       uint64
}

// RollupEventL1UserTx is an event of the Rollup Smart Contract
type RollupEventL1UserTx struct {
	ToForgeL1TxsNum int64 // QueueIndex       *big.Int
	Position        int   // TransactionIndex *big.Int
	L1UserTx        common.L1Tx
}

// RollupEventL1UserTxAux is an event of the Rollup Smart Contract
type rollupEventL1UserTxAux struct {
	ToForgeL1TxsNum uint64 // QueueIndex       *big.Int
	Position        uint8  // TransactionIndex *big.Int
	L1UserTx        []byte
}

// RollupEventForgeBatch is an event of the Rollup Smart Contract
type RollupEventForgeBatch struct {
	BatchNum int64
	// Sender    ethCommon.Address
	EthTxHash    ethCommon.Hash
	L1UserTxsLen uint16
	GasUsed      uint64
	GasPrice     *big.Int
}

// RollupEventUpdateForgeL1L2BatchTimeout is an event of the Rollup Smart Contract
type RollupEventUpdateForgeL1L2BatchTimeout struct {
	NewForgeL1L2BatchTimeout int64
}

// RollupEventUpdateFeeAddToken is an event of the Rollup Smart Contract
type RollupEventUpdateFeeAddToken struct {
	NewFeeAddToken *big.Int
}

// RollupEventWithdraw is an event of the Rollup Smart Contract
type RollupEventWithdraw struct {
	Idx             uint64
	NumExitRoot     uint64
	InstantWithdraw bool
	TxHash          ethCommon.Hash // Hash of the transaction that generated this event
}

// RollupEventUpdateBucketWithdraw is an event of the Rollup Smart Contract
type RollupEventUpdateBucketWithdraw struct {
	NumBucket   int
	BlockStamp  int64 // blockNum
	Withdrawals *big.Int
}

type rollupEventUpdateBucketWithdrawAux struct {
	NumBucket   uint8
	BlockStamp  *big.Int
	Withdrawals *big.Int
}

// RollupEventUpdateWithdrawalDelay is an event of the Rollup Smart Contract
type RollupEventUpdateWithdrawalDelay struct {
	NewWithdrawalDelay uint64
}

// RollupUpdateBucketsParameters are the bucket parameters used in an update
type RollupUpdateBucketsParameters struct {
	CeilUSD         *big.Int
	BlockStamp      *big.Int
	Withdrawals     *big.Int
	RateBlocks      *big.Int
	RateWithdrawals *big.Int
	MaxWithdrawals  *big.Int
}

// type rollupEventUpdateBucketsParametersAux struct {
// 	ArrayBuckets []*big.Int
// }

// RollupEventUpdateBucketsParameters is an event of the Rollup Smart Contract
type RollupEventUpdateBucketsParameters struct {
	ArrayBuckets []RollupUpdateBucketsParameters
	SafeMode     bool
}

// RollupEventUpdateTokenExchange is an event of the Rollup Smart Contract
type RollupEventUpdateTokenExchange struct {
	AddressArray []ethCommon.Address
	ValueArray   []uint64
}

// RollupEventSafeMode is an event of the Rollup Smart Contract
type RollupEventSafeMode struct{}

// RollupEvents is the list of events in a block of the Rollup Smart Contract
type RollupEvents struct {
	L1UserTx                    []RollupEventL1UserTx
	ForgeBatch                  []RollupEventForgeBatch
	UpdateForgeL1L2BatchTimeout []RollupEventUpdateForgeL1L2BatchTimeout
	UpdateFeeAddToken           []RollupEventUpdateFeeAddToken
	Withdraw                    []RollupEventWithdraw
	UpdateWithdrawalDelay       []RollupEventUpdateWithdrawalDelay
	UpdateBucketWithdraw        []RollupEventUpdateBucketWithdraw
	UpdateBucketsParameters     []RollupEventUpdateBucketsParameters
	UpdateTokenExchange         []RollupEventUpdateTokenExchange
	SafeMode                    []RollupEventSafeMode
}

// NewRollupEvents creates an empty RollupEvents with the slices initialized.
func NewRollupEvents() RollupEvents {
	return RollupEvents{
		L1UserTx:                    make([]RollupEventL1UserTx, 0),
		ForgeBatch:                  make([]RollupEventForgeBatch, 0),
		UpdateForgeL1L2BatchTimeout: make([]RollupEventUpdateForgeL1L2BatchTimeout, 0),
		UpdateFeeAddToken:           make([]RollupEventUpdateFeeAddToken, 0),
		Withdraw:                    make([]RollupEventWithdraw, 0),
	}
}

// RollupForgeBatchArgs are the arguments to the ForgeBatch function in the Rollup Smart Contract
type RollupForgeBatchArgs struct {
	NewLastIdx            int64
	NewAccountRoot        *big.Int
	NewScoreRoot          *big.Int
	NewVouchRoot          *big.Int
	NewExitRoot           *big.Int
	L1UserTxs             []common.L1Tx
	L1CoordinatorTxs      []common.L1Tx
	L1CoordinatorTxsAuths [][]byte // Authorization for accountCreations for each L1CoordinatorTx
	// Circuit selector
	VerifierIdx uint8
	L1Batch     bool
	ProofA      [2]*big.Int
	ProofB      [2][2]*big.Int
	ProofC      [2]*big.Int
}

// RollupForgeBatchArgsAux are the arguments to the ForgeBatch function in the Rollup Smart Contract
type rollupForgeBatchArgsAux struct {
	NewLastIdx             *big.Int
	NewAccountRoot         *big.Int
	NewVouchRoot           *big.Int
	NewScoreRoot           *big.Int
	NewExitRoot            *big.Int
	EncodedL1CoordinatorTx []byte
	L1L2TxsData            []byte
	FeeIdxCoordinator      []byte
	// Circuit selector
	VerifierIdx uint8
	L1Batch     bool
	ProofA      [2]*big.Int
	ProofB      [2][2]*big.Int
	ProofC      [2]*big.Int
}

// TODO: Update interfaces and the functions
// RollupInterface is the inteface to to Rollup Smart Contract
type RollupInterface interface {
	//
	// Smart Contract Methods
	//

	// Public Functions

	RollupForgeBatch(*RollupForgeBatchArgs, *bind.TransactOpts) (*types.Transaction, error)

	// RollupWithdrawMerkleProof(babyPubKey babyjub.PublicKeyComp, tokenID uint32, numExitRoot,
	// 	idx int64, amount *big.Int, siblings []*big.Int, instantWithdraw bool) (*types.Transaction,
	// 	error)
	// RollupWithdrawCircuit(proofA, proofC [2]*big.Int, proofB [2][2]*big.Int, tokenID uint32,
	// 	numExitRoot, idx int64, amount *big.Int, instantWithdraw bool) (*types.Transaction, error)

	// Governance Public Functions
	// RollupUpdateForgeL1L2BatchTimeout(newForgeL1L2BatchTimeout int64) (*types.Transaction, error)

	// Viewers
	RollupLastForgedBatch() (int64, error)

	//
	// Smart Contract Status
	//

	RollupConstants() (*common.RollupConstants, error)
	RollupEventsByBlock(blockNum int64, blockHash *ethCommon.Hash) (*RollupEvents, error)
	RollupForgeBatchArgs(ethCommon.Hash, uint16) (*RollupForgeBatchArgs, *ethCommon.Address, error)
	RollupEventInit(genesisBlockNum int64) (*RollupEventInitialize, int64, error)
}

//
// Implementation
//

// RollupClient is the implementation of the interface to the Rollup Smart Contract in ethereum.
type RollupClient struct {
	client      *EthereumClient
	chainID     *big.Int
	address     ethCommon.Address
	sybil       *Sybil.Sybil
	contractAbi abi.ABI
	opts        *bind.CallOpts
	consts      *common.RollupConstants
}

// RollupVariables returns the RollupVariables from the initialize event
func (ei *RollupEventInitialize) RollupVariables() *common.RollupVariables {
	return &common.RollupVariables{
		EthBlockNum:           0,
		ForgeL1L2BatchTimeout: int64(ei.ForgeL1L2BatchTimeout),
		Buckets:               []common.BucketParams{},
		SafeMode:              false,
	}
}

// RollupEventInit returns the initialize event with its corresponding block number
func (c *RollupClient) RollupEventInit(genesisBlockNum int64) (*RollupEventInitialize, int64, error) {
	query := ethereum.FilterQuery{
		Addresses: []ethCommon.Address{
			c.address,
		},
		FromBlock: big.NewInt(max(0, genesisBlockNum-blocksPerDay)),
		ToBlock:   big.NewInt(genesisBlockNum),
		Topics:    [][]ethCommon.Hash{{logSYBInitialize}},
	}
	logs, err := c.client.client.FilterLogs(context.Background(), query)
	if err != nil {
		return nil, 0, common.Wrap(err)
	}
	if len(logs) != 1 {
		return nil, 0, common.Wrap(fmt.Errorf("no event of type InitializeSYBEvent found"))
	}
	vLog := logs[0]
	if vLog.Topics[0] != logSYBInitialize {
		return nil, 0, common.Wrap(fmt.Errorf("event is not InitializeSYBEvent"))
	}

	var rollupInit RollupEventInitialize
	if err := c.contractAbi.UnpackIntoInterface(&rollupInit, "Initialize",
		vLog.Data); err != nil {
		return nil, 0, common.Wrap(err)
	}
	return &rollupInit, int64(vLog.BlockNumber), common.Wrap(err)
}

// NewRollupClient creates a new RollupClient
func NewRollupClient(client *EthereumClient, address ethCommon.Address) (*RollupClient, error) {
	contractAbi, err := abi.JSON(strings.NewReader(string(Sybil.SybilABI)))
	if err != nil {
		return nil, common.Wrap(err)
	}
	sybil, err := Sybil.NewSybil(address, client.Client())
	if err != nil {
		return nil, common.Wrap(err)
	}
	chainID, err := client.EthChainID()
	if err != nil {
		return nil, common.Wrap(err)
	}
	c := &RollupClient{
		client:      client,
		chainID:     chainID,
		address:     address,
		sybil:       sybil,
		contractAbi: contractAbi,
		opts:        newCallOpts(),
	}
	consts, err := c.RollupConstants()
	if err != nil {
		return nil, common.Wrap(fmt.Errorf("RollupConstants at %v: %w", address, err))
	}
	c.consts = consts
	return c, nil
}

// RollupConstants returns the Constants of the Rollup Smart Contract
func (c *RollupClient) RollupConstants() (rollupConstants *common.RollupConstants, err error) {
	rollupConstants = new(common.RollupConstants)
	if err := c.client.Call(func(ec *ethclient.Client) error {
		absoluteMaxL1L2BatchTimeout, err := c.sybil.ABSOLUTEMAXBATCHTIMEOUT(c.opts)
		if err != nil {
			return common.Wrap(err)
		}
		rollupConstants.AbsoluteMaxL1L2BatchTimeout = int64(absoluteMaxL1L2BatchTimeout)
		// rollupConstants.TokenHEZ, err = c.tokamak.TokenHEZ(c.opts)
		// if err != nil {
		// 	return common.Wrap(err)
		// }
		rollupVerifier, err := c.sybil.RollupVerifier(c.opts)
		if err != nil {
			return common.Wrap(err)
		}
		// for i := int64(0); i < rollupVerifiers.MaxTxs.Int64(); i++ {
		// 	var newRollupVerifier common.RollupVerifierStruct
		// 	rollupVerifier, err := c.sybil.RollupVerifiers(c.opts, big.NewInt(i))
		// 	if err != nil {
		// 		return common.Wrap(err)
		// 	}
		// 	newRollupVerifier.MaxTx = rollupVerifier.MaxTxs.Int64()
		// 	newRollupVerifier.NLevels = rollupVerifier.NLevels.Int64()
		// 	rollupConstants.Verifiers = append(rollupConstants.Verifiers,
		// 		newRollupVerifier)
		// }
		var newRollupVerifier common.RollupVerifierStruct
		newRollupVerifier.MaxTx = rollupVerifier.MaxTx.Int64()
		newRollupVerifier.NLevels = rollupVerifier.NLevel.Int64()
		rollupConstants.Verifiers = append(rollupConstants.Verifiers,
			newRollupVerifier)
		return common.Wrap(err)
	}); err != nil {
		return nil, common.Wrap(err)
	}
	return rollupConstants, nil
}

// RollupLastForgedBatch is the interface to call the smart contract function
func (c *RollupClient) RollupLastForgedBatch() (lastForgedBatch int64, err error) {
	if err := c.client.Call(func(ec *ethclient.Client) error {
		_lastForgedBatch, err := c.sybil.LastForgedBatch(c.opts)
		lastForgedBatch = int64(_lastForgedBatch)
		return common.Wrap(err)
	}); err != nil {
		return 0, common.Wrap(err)
	}
	return lastForgedBatch, nil
}

var (
	logSYBL1UserTxEvent = crypto.Keccak256Hash([]byte(
		"L1UserTxEvent(uint32,uint8,bytes)"))
	logSYBForgeBatch = crypto.Keccak256Hash([]byte(
		"ForgeBatch(uint32,uint16)"))
	logSYBUpdateForgeL1L2BatchTimeout = crypto.Keccak256Hash([]byte(
		"UpdateForgeL1L2BatchTimeout(uint8)"))
	logSYBWithdrawEvent = crypto.Keccak256Hash([]byte(
		"WithdrawEvent(uint48,uint32,bool)"))
	logSYBUpdateBucketWithdraw = crypto.Keccak256Hash([]byte(
		"UpdateBucketWithdraw(uint8,uint256,uint256)"))
	// logSYBUpdateBucketsParameters = crypto.Keccak256Hash([]byte(
	// 	"UpdateBucketsParameters(uint256[])"))
	logSYBSafeMode = crypto.Keccak256Hash([]byte(
		"SafeMode()"))
	logSYBInitialize = crypto.Keccak256Hash([]byte(
		"Initialize(uint8)"))
)

// RollupEventsByBlock returns the events in a block that happened in the
// Rollup Smart Contract.
// To query by blockNum, set blockNum >= 0 and blockHash == nil.
// To query by blockHash set blockHash != nil, and blockNum will be ignored.
// If there are no events in that block the result is nil.
func (c *RollupClient) RollupEventsByBlock(blockNum int64,
	blockHash *ethCommon.Hash) (*RollupEvents, error) {
	var rollupEvents RollupEvents

	var blockNumBigInt *big.Int
	if blockHash == nil {
		blockNumBigInt = big.NewInt(blockNum)
	}
	query := ethereum.FilterQuery{
		BlockHash: blockHash,
		FromBlock: blockNumBigInt,
		ToBlock:   blockNumBigInt,
		Addresses: []ethCommon.Address{
			c.address,
		},
		Topics: [][]ethCommon.Hash{},
	}
	logs, err := c.client.client.FilterLogs(context.Background(), query)
	if err != nil {
		return nil, common.Wrap(err)
	}
	if len(logs) == 0 {
		return nil, nil
	}

	for _, vLog := range logs {
		if blockHash != nil && vLog.BlockHash != *blockHash {
			log.Errorw("Block hash mismatch", "expected", blockHash.String(), "got", vLog.BlockHash.String())
			return nil, common.Wrap(ErrBlockHashMismatchEvent)
		}
		switch vLog.Topics[0] {
		case logSYBL1UserTxEvent:
			var L1UserTxAux rollupEventL1UserTxAux
			var L1UserTx RollupEventL1UserTx
			err := c.contractAbi.UnpackIntoInterface(&L1UserTxAux, "L1UserTxEvent", vLog.Data)
			if err != nil {
				return nil, common.Wrap(err)
			}
			L1Tx, err := common.L1UserTxFromBytes(L1UserTxAux.L1UserTx)
			if err != nil {
				return nil, common.Wrap(err)
			}
			toForgeL1TxsNum := new(big.Int).SetBytes(vLog.Topics[1][:]).Int64()
			L1Tx.ToForgeL1TxsNum = &toForgeL1TxsNum
			L1Tx.Position = int(new(big.Int).SetBytes(vLog.Topics[2][:]).Int64())
			L1Tx.UserOrigin = true
			L1Tx.EthTxHash = vLog.TxHash
			//Get l1Fee in eth wei spent in the l1 tx
			tx, _, err := c.client.client.TransactionByHash(context.Background(), vLog.TxHash)
			if err != nil {
				return nil, common.Wrap(fmt.Errorf("failed to get TransactionByHash, hash: %s, err: %w", vLog.TxHash.String(), err))
			}
			l1Fee := new(big.Int).Mul(tx.GasPrice(), new(big.Int).SetUint64(tx.Gas()))
			L1Tx.L1Fee = l1Fee
			L1UserTx.L1UserTx = *L1Tx
			rollupEvents.L1UserTx = append(rollupEvents.L1UserTx, L1UserTx)
		case logSYBForgeBatch:
			var forgeBatch RollupEventForgeBatch
			err := c.contractAbi.UnpackIntoInterface(&forgeBatch, "ForgeBatch", vLog.Data)
			if err != nil {
				return nil, common.Wrap(err)
			}
			forgeBatch.BatchNum = new(big.Int).SetBytes(vLog.Topics[1][:]).Int64()
			forgeBatch.EthTxHash = vLog.TxHash
			//Check tx info using EthTxHash to get gasprice and gas used
			tx, _, err := c.client.client.TransactionByHash(context.Background(), vLog.TxHash)
			if err != nil {
				return nil, common.Wrap(fmt.Errorf("failed to get TransactionByHash, hash: %s, err: %w", vLog.TxHash.String(), err))
			}
			forgeBatch.GasPrice = tx.GasPrice()
			// Get gas used from TxReceipt
			txReceipt, err := c.client.client.TransactionReceipt(context.Background(), vLog.TxHash)
			if err != nil {
				return nil, common.Wrap(fmt.Errorf("failed to get TransactionByHash, hash: %s, err: %w", vLog.TxHash.String(), err))
			}
			forgeBatch.GasUsed = txReceipt.GasUsed
			rollupEvents.ForgeBatch = append(rollupEvents.ForgeBatch, forgeBatch)
		case logSYBUpdateForgeL1L2BatchTimeout:
			var updateForgeL1L2BatchTimeout struct {
				NewForgeL1L2BatchTimeout uint8
			}
			err := c.contractAbi.UnpackIntoInterface(&updateForgeL1L2BatchTimeout,
				"UpdateForgeL1L2BatchTimeout", vLog.Data)
			if err != nil {
				return nil, common.Wrap(err)
			}
			rollupEvents.UpdateForgeL1L2BatchTimeout = append(rollupEvents.UpdateForgeL1L2BatchTimeout,
				RollupEventUpdateForgeL1L2BatchTimeout{
					NewForgeL1L2BatchTimeout: int64(updateForgeL1L2BatchTimeout.NewForgeL1L2BatchTimeout),
				})
		case logSYBWithdrawEvent:
			var withdraw RollupEventWithdraw
			withdraw.Idx = new(big.Int).SetBytes(vLog.Topics[1][:]).Uint64()
			withdraw.NumExitRoot = new(big.Int).SetBytes(vLog.Topics[2][:]).Uint64()
			instantWithdraw := new(big.Int).SetBytes(vLog.Topics[3][:]).Uint64()
			if instantWithdraw == 1 {
				withdraw.InstantWithdraw = true
			}
			withdraw.TxHash = vLog.TxHash
			rollupEvents.Withdraw = append(rollupEvents.Withdraw, withdraw)
		case logSYBUpdateBucketWithdraw:
			var updateBucketWithdrawAux rollupEventUpdateBucketWithdrawAux
			var updateBucketWithdraw RollupEventUpdateBucketWithdraw
			err := c.contractAbi.UnpackIntoInterface(&updateBucketWithdrawAux,
				"UpdateBucketWithdraw", vLog.Data)
			if err != nil {
				return nil, common.Wrap(err)
			}
			updateBucketWithdraw.Withdrawals = updateBucketWithdrawAux.Withdrawals
			updateBucketWithdraw.NumBucket = int(new(big.Int).SetBytes(vLog.Topics[1][:]).Int64())
			updateBucketWithdraw.BlockStamp = new(big.Int).SetBytes(vLog.Topics[2][:]).Int64()
			rollupEvents.UpdateBucketWithdraw =
				append(rollupEvents.UpdateBucketWithdraw, updateBucketWithdraw)
		// case logSYBUpdateBucketsParameters:
		// 	var bucketsParametersAux rollupEventUpdateBucketsParametersAux
		// 	var bucketsParameters RollupEventUpdateBucketsParameters
		// 	err := c.contractAbi.UnpackIntoInterface(&bucketsParametersAux,
		// 		"UpdateBucketsParameters", vLog.Data)
		// 	if err != nil {
		// 		return nil, common.Wrap(err)
		// 	}
		// 	bucketsParameters.ArrayBuckets = make([]RollupUpdateBucketsParameters, len(bucketsParametersAux.ArrayBuckets))
		// 	for i, bucket := range bucketsParametersAux.ArrayBuckets {
		// 		bucket, err := c.hermez.UnpackBucket(c.opts, bucket)
		// 		if err != nil {
		// 			return nil, common.Wrap(err)
		// 		}
		// 		bucketsParameters.ArrayBuckets[i].CeilUSD = bucket.CeilUSD
		// 		bucketsParameters.ArrayBuckets[i].BlockStamp = bucket.BlockStamp
		// 		bucketsParameters.ArrayBuckets[i].Withdrawals = bucket.Withdrawals
		// 		bucketsParameters.ArrayBuckets[i].RateBlocks = bucket.RateBlocks
		// 		bucketsParameters.ArrayBuckets[i].RateWithdrawals = bucket.RateWithdrawals
		// 		bucketsParameters.ArrayBuckets[i].MaxWithdrawals = bucket.MaxWithdrawals
		// 	}
		// 	rollupEvents.UpdateBucketsParameters =
		// 		append(rollupEvents.UpdateBucketsParameters, bucketsParameters)
		case logSYBSafeMode:
			var safeMode RollupEventSafeMode
			rollupEvents.SafeMode = append(rollupEvents.SafeMode, safeMode)
			// Also add an UpdateBucketsParameter with
			// SafeMode=true to keep the order between `safeMode`
			// and `UpdateBucketsParameters`
			bucketsParameters := RollupEventUpdateBucketsParameters{
				SafeMode: true,
			}
			for i := range bucketsParameters.ArrayBuckets {
				bucketsParameters.ArrayBuckets[i].CeilUSD = big.NewInt(0)
				bucketsParameters.ArrayBuckets[i].BlockStamp = big.NewInt(0)
				bucketsParameters.ArrayBuckets[i].Withdrawals = big.NewInt(0)
				bucketsParameters.ArrayBuckets[i].RateBlocks = big.NewInt(0)
				bucketsParameters.ArrayBuckets[i].RateWithdrawals = big.NewInt(0)
				bucketsParameters.ArrayBuckets[i].MaxWithdrawals = big.NewInt(0)
			}
			rollupEvents.UpdateBucketsParameters = append(rollupEvents.UpdateBucketsParameters,
				bucketsParameters)
		}
	}
	return &rollupEvents, nil
}

// RollupForgeBatch is the interface to call the smart contract function
func (c *RollupClient) RollupForgeBatch(args *RollupForgeBatchArgs, auth *bind.TransactOpts) (tx *types.Transaction, err error) {
	if auth == nil {
		auth, err = c.client.NewAuth()
		if err != nil {
			return nil, err
		}
		auth.GasLimit = 1000000
	}

	// nLevels := c.consts.Verifiers[args.VerifierIdx].NLevels //check verifiers

	newLastIdx := big.NewInt(int64(args.NewLastIdx))

	// var l1TxData []byte
	// for i := 0; i < len(args.L1UserTxs); i++ {
	// 	l1User := args.L1UserTxs[i]
	// 	bytesl1User, err := l1User.BytesDataAvailability(uint32(nLevels))
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	l1TxData = append(l1TxData, bytesl1User[:]...)
	// }

	// TODO: Need to send ZK Proof here on last param
	tx, err = c.sybil.ForgeBatch(
		auth,
		newLastIdx,
		args.NewAccountRoot,
		args.NewVouchRoot,
		args.NewScoreRoot,
		args.NewExitRoot,
		args.ProofA,
		args.ProofB,
		args.ProofC,
	)
	if err != nil {
		return nil, fmt.Errorf("Sybil.ForgeBatch: %w", err)
	}
	return tx, nil
}

// RollupForgeBatchArgs returns the arguments used in a ForgeBatch call in the
// Rollup Smart Contract in the given transaction, and the sender address.
func (c *RollupClient) RollupForgeBatchArgs(ethTxHash ethCommon.Hash,
	l1UserTxsLen uint16) (*RollupForgeBatchArgs, *ethCommon.Address, error) {
	tx, _, err := c.client.client.TransactionByHash(context.Background(), ethTxHash)
	if err != nil {
		return nil, nil, common.Wrap(fmt.Errorf("TransactionByHash: %w", err))
	}
	txData := tx.Data()

	method, err := c.contractAbi.MethodById(txData[:4])
	if err != nil {
		return nil, nil, common.Wrap(err)
	}
	receipt, err := c.client.client.TransactionReceipt(context.Background(), ethTxHash)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}
	sender, err := c.client.client.TransactionSender(context.Background(), tx,
		receipt.Logs[0].BlockHash, receipt.Logs[0].Index)
	if err != nil {
		return nil, nil, common.Wrap(err)
	}
	var aux rollupForgeBatchArgsAux
	if values, err := method.Inputs.Unpack(txData[4:]); err != nil {
		return nil, nil, common.Wrap(err)
	} else if err := method.Inputs.Copy(&aux, values); err != nil {
		return nil, nil, common.Wrap(err)
	}
	rollupForgeBatchArgs := RollupForgeBatchArgs{
		L1Batch:               aux.L1Batch,
		NewExitRoot:           aux.NewExitRoot,
		NewLastIdx:            aux.NewLastIdx.Int64(),
		NewAccountRoot:        aux.NewAccountRoot,
		NewVouchRoot:          aux.NewVouchRoot,
		NewScoreRoot:          aux.NewScoreRoot,
		ProofA:                aux.ProofA,
		ProofB:                aux.ProofB,
		ProofC:                aux.ProofC,
		VerifierIdx:           aux.VerifierIdx,
		L1CoordinatorTxs:      []common.L1Tx{},
		L1CoordinatorTxsAuths: [][]byte{},
	}
	nLevels := c.consts.Verifiers[rollupForgeBatchArgs.VerifierIdx].NLevels
	lenL1L2TxsBytes := int((nLevels/8)*2 + common.Float40BytesLength + 1) //nolint:gomnd
	numBytesL1TxUser := int(l1UserTxsLen) * lenL1L2TxsBytes
	numTxsL1Coord := len(aux.EncodedL1CoordinatorTx) / common.RollupConstL1CoordinatorTotalBytes
	l1UserTxsData := []byte{}
	if l1UserTxsLen > 0 {
		l1UserTxsData = aux.L1L2TxsData[:numBytesL1TxUser]
	}
	for i := 0; i < int(l1UserTxsLen); i++ {
		l1Tx, err :=
			common.L1TxFromDataAvailability(l1UserTxsData[i*lenL1L2TxsBytes:(i+1)*lenL1L2TxsBytes],
				uint32(nLevels))
		if err != nil {
			return nil, nil, common.Wrap(err)
		}
		rollupForgeBatchArgs.L1UserTxs = append(rollupForgeBatchArgs.L1UserTxs, *l1Tx)
	}
	for i := 0; i < numTxsL1Coord; i++ {
		bytesL1Coordinator :=
			aux.EncodedL1CoordinatorTx[i*common.RollupConstL1CoordinatorTotalBytes : (i+1)*common.RollupConstL1CoordinatorTotalBytes] //nolint:lll
		var signature []byte
		v := bytesL1Coordinator[0]
		s := bytesL1Coordinator[1:33]
		r := bytesL1Coordinator[33:65]
		signature = append(signature, r[:]...)
		signature = append(signature, s[:]...)
		signature = append(signature, v)
		l1Tx, err := common.L1CoordinatorTxFromBytes(bytesL1Coordinator, c.chainID, c.address)
		if err != nil {
			return nil, nil, common.Wrap(err)
		}
		rollupForgeBatchArgs.L1CoordinatorTxs = append(rollupForgeBatchArgs.L1CoordinatorTxs, *l1Tx)
		rollupForgeBatchArgs.L1CoordinatorTxsAuths =
			append(rollupForgeBatchArgs.L1CoordinatorTxsAuths, signature)
	}
	lenFeeIdxCoordinatorBytes := int(nLevels / 8) //nolint:gomnd
	numFeeIdxCoordinator := len(aux.FeeIdxCoordinator) / lenFeeIdxCoordinatorBytes
	for i := 0; i < numFeeIdxCoordinator; i++ {
		var paddedFeeIdx [6]byte
		if lenFeeIdxCoordinatorBytes < common.AccountIdxBytesLen {
			copy(paddedFeeIdx[6-lenFeeIdxCoordinatorBytes:],
				aux.FeeIdxCoordinator[i*lenFeeIdxCoordinatorBytes:(i+1)*lenFeeIdxCoordinatorBytes])
		} else {
			copy(paddedFeeIdx[:],
				aux.FeeIdxCoordinator[i*lenFeeIdxCoordinatorBytes:(i+1)*lenFeeIdxCoordinatorBytes])
		}
	}
	return &rollupForgeBatchArgs, &sender, nil
}
