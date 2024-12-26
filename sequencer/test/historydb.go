package test

import (
	"math/big"
	"time"
	"tokamak-sybil-resistance/common"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-merkletree"
)

// WARNING: the generators in this file doesn't necessary follow the protocol
// they are intended to check that the parsers between struct <==> DB are correct

// GenBlocks generates block from, to block numbers. WARNING: This is meant for DB/API testing, and
// may not be fully consistent with the protocol.
func GenBlocks(from, to int64) []common.Block {
	var blocks []common.Block
	for i := from; i < to; i++ {
		blocks = append(blocks, common.Block{
			Num: i,
			//nolint:gomnd
			Timestamp: time.Now().Add(time.Second * 13).UTC(),
			Hash:      ethCommon.BigToHash(big.NewInt(int64(i))),
		})
	}
	return blocks
}

// GenAccounts generates accounts. WARNING: This is meant for DB/API testing, and may not be fully
// consistent with the protocol.
func GenAccounts(totalAccounts, userAccounts int,
	userAddr *ethCommon.Address, userBjj *babyjub.PublicKey,
	batches []common.Batch) []common.Account {
	if totalAccounts < userAccounts {
		panic("totalAccounts must be greater than userAccounts")
	}
	accs := []common.Account{}
	for i := 256; i < 256+totalAccounts; i++ {
		var addr ethCommon.Address
		var pubK *babyjub.PublicKey
		if i < 256+userAccounts {
			addr = *userAddr
			pubK = userBjj
		} else {
			addr = ethCommon.BigToAddress(big.NewInt(int64(i)))
			privK := babyjub.NewRandPrivKey()
			pubK = privK.Public()
		}
		accs = append(accs, common.Account{
			Idx:      common.AccountIdx(i),
			EthAddr:  addr,
			BatchNum: batches[i%len(batches)].BatchNum,
			BJJ:      pubK.Compress(),
			Balance:  big.NewInt(int64(i * 10000000)), //nolint:gomnd
		})
	}
	return accs
}

// GenBatches generates batches. WARNING: This is meant for DB/API testing, and may not be fully
// consistent with the protocol.
func GenBatches(nBatches int, blocks []common.Block) []common.Batch {
	batches := []common.Batch{}
	// collectedFees := make(map[common.TokenID]*big.Int)
	// for i := 0; i < 64; i++ {
	// 	collectedFees[common.TokenID(i)] = big.NewInt(int64(i))
	// }
	for i := 0; i < nBatches; i++ {
		batch := common.Batch{
			BatchNum:    common.BatchNum(i + 1),
			EthBlockNum: blocks[i%len(blocks)].Num,
			//nolint:gomnd
			ForgerAddr: ethCommon.BigToAddress(big.NewInt(6886723)),

			AccountRoot: big.NewInt(int64(i+1) * 5), //nolint:gomnd
			VouchRoot:   big.NewInt(int64(i+1) * 6), //nolint:gomnd
			ScoreRoot:   big.NewInt(int64(i+1) * 7), //nolint:gomnd
			//nolint:gomnd
			NumAccounts: 30,
			ExitRoot:    big.NewInt(int64(i+1) * 16), //nolint:gomnd
			SlotNum:     int64(i),
			GasPrice:    big.NewInt(0),
		}
		if i%2 == 0 {
			toForge := new(int64)
			*toForge = int64(i + 1)
			batch.ForgeL1TxsNum = toForge
		}
		batches = append(batches, batch)
	}
	return batches
}

// GenExitTree generates an exitTree (as an array of Exits)
//
//nolint:gomnd
func GenExitTree(
	n int,
	batches []common.Batch,
	accounts []common.Account,
	blocks []common.Block,
) []common.ExitInfo {
	exitTree := make([]common.ExitInfo, n)
	for i := 0; i < n; i++ {
		exitTree[i] = common.ExitInfo{
			BatchNum:               batches[i%len(batches)].BatchNum,
			InstantWithdrawn:       nil,
			DelayedWithdrawRequest: nil,
			DelayedWithdrawn:       nil,
			AccountIdx:             accounts[i%len(accounts)].Idx,
			MerkleProof: &merkletree.CircomVerifierProof{
				Root: &merkletree.Hash{byte(i), byte(i + 1)},
				Siblings: []*merkletree.Hash{
					merkletree.NewHashFromBigInt(big.NewInt(int64(i) * 10)),
					merkletree.NewHashFromBigInt(big.NewInt(int64(i)*100 + 1)),
					merkletree.NewHashFromBigInt(big.NewInt(int64(i)*1000 + 2))},
				OldKey:   &merkletree.Hash{byte(i * 1), byte(i*1 + 1)},
				OldValue: &merkletree.Hash{byte(i * 2), byte(i*2 + 1)},
				IsOld0:   i%2 == 0,
				Key:      &merkletree.Hash{byte(i * 3), byte(i*3 + 1)},
				Value:    &merkletree.Hash{byte(i * 4), byte(i*4 + 1)},
				Fnc:      i % 2,
			},
			Balance: big.NewInt(int64(i) * 1000),
		}
		if i%2 == 0 {
			instant := int64(blocks[i%len(blocks)].Num)
			exitTree[i].InstantWithdrawn = &instant
		} else if i%3 == 0 {
			delayedReq := int64(blocks[i%len(blocks)].Num)
			exitTree[i].DelayedWithdrawRequest = &delayedReq
			if i%9 == 0 {
				delayed := int64(blocks[i%len(blocks)].Num)
				exitTree[i].DelayedWithdrawn = &delayed
			}
		}
	}
	return exitTree
}
