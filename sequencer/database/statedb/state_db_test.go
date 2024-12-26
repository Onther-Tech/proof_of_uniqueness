package statedb

import (
	"encoding/hex"
	"math/big"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/log"

	ethCommon "github.com/ethereum/go-ethereum/common"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-merkletree/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var deleteme []string

func init() {
	log.Init("debug", []string{"stdout"})
}
func TestMain(m *testing.M) {
	exitVal := 0
	exitVal = m.Run()
	for _, dir := range deleteme {
		if err := os.RemoveAll(dir); err != nil {
			panic(err)
		}
	}
	os.Exit(exitVal)
}

func newAccount(t *testing.T, i int) *common.Account {
	var sk babyjub.PrivateKey
	_, err := hex.Decode(sk[:],
		[]byte("0001020304050607080900010203040506070809000102030405060708090001"))
	require.NoError(t, err)
	pk := sk.Public()

	key, err := ethCrypto.GenerateKey()
	require.NoError(t, err)
	address := ethCrypto.PubkeyToAddress(key.PublicKey)

	return &common.Account{
		Idx:     common.AccountIdx(256 + i),
		Nonce:   common.Nonce(i),
		Balance: big.NewInt(1000),
		BJJ:     pk.Compress(),
		EthAddr: address,
	}
}

func newVouch(i int) *common.Vouch {
	r := rand.New(rand.NewSource(int64(time.Now().UnixNano())))
	v := r.Intn(2) == 1
	return &common.Vouch{
		Idx:   common.VouchIdx(256257 + i),
		Value: v,
	}
}

func newScore(i int) *common.Score {
	return &common.Score{
		Idx:   common.AccountIdx(256 + i),
		Value: uint32(1 + i),
	}
}

func TestAccountInStateDB(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	sdb, err := NewStateDB(Config{Path: dir, Keep: 128, Type: TypeSynchronizer, NLevels: 0})
	require.NoError(t, err)

	// create test accounts
	var accounts []*common.Account
	for i := 0; i < 4; i++ {
		accounts = append(accounts, newAccount(t, i))
	}

	// get non-existing account, expecting an error
	unexistingAccount := common.AccountIdx(1)
	_, err = sdb.GetAccount(unexistingAccount)
	assert.NotNil(t, err)
	assert.Equal(t, db.ErrNotFound, common.Unwrap(err))

	// add test accounts
	for i := 0; i < len(accounts); i++ {
		_, err = sdb.CreateAccount(accounts[i].Idx, accounts[i])
		require.NoError(t, err)
	}

	for i := 0; i < len(accounts); i++ {
		existingAccount := accounts[i].Idx
		accGetted, err := sdb.GetAccount(existingAccount)
		require.NoError(t, err)
		assert.Equal(t, accounts[i], accGetted)
	}

	// try already existing idx and get error
	existingAccount := common.AccountIdx(256)
	_, err = sdb.GetAccount(existingAccount) // check that exist
	require.NoError(t, err)
	_, err = sdb.CreateAccount(common.AccountIdx(256), accounts[1]) // check that can not be created twice
	assert.NotNil(t, err)
	assert.Equal(t, ErrAccountAlreadyExists, common.Unwrap(err))

	_, err = sdb.MTGetAccountProof(common.AccountIdx(256))
	require.NoError(t, err)

	// update accountsCreateAccount
	for i := 0; i < len(accounts); i++ {
		accounts[i].Nonce = accounts[i].Nonce + 1
		existingAccount = accounts[i].Idx
		_, err = sdb.UpdateAccount(existingAccount, accounts[i])
		require.NoError(t, err)
	}

	sdb.Close()
}

func TestVouchInStateDB(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	sdb, err := NewStateDB(Config{Path: dir, Keep: 128, Type: TypeSynchronizer, NLevels: 32})
	require.NoError(t, err)

	// create test vouches
	var vouches []*common.Vouch
	for i := 0; i < 4; i++ {
		vouches = append(vouches, newVouch(i))
	}

	// get non-existing vouch, expecting an error
	unexistingVouch := common.VouchIdx(001001)
	_, err = sdb.GetVouch(unexistingVouch)
	assert.NotNil(t, err)
	assert.Equal(t, db.ErrNotFound, common.Unwrap(err))

	// add test vouches
	for i := 0; i < len(vouches); i++ {
		_, err = sdb.CreateVouch(vouches[i].Idx, vouches[i])
		require.NoError(t, err)
	}

	for i := 0; i < len(vouches); i++ {
		existingVouch := vouches[i].Idx
		vocGetted, err := sdb.GetVouch(existingVouch)
		require.NoError(t, err)
		assert.Equal(t, vouches[i], vocGetted)
	}

	// try already existing idx and get error
	existingVouch := common.VouchIdx(256257)
	_, err = sdb.GetVouch(existingVouch) // check that exist
	require.NoError(t, err)
	_, err = sdb.CreateVouch(common.VouchIdx(256257), vouches[1]) // check that can not be created twice
	assert.NotNil(t, err)
	assert.Equal(t, ErrAlreadyVouched, common.Unwrap(err))

	_, err = sdb.MTGetVouchProof(common.VouchIdx(256257))
	require.NoError(t, err)

	// update vouches
	for i := 0; i < len(vouches); i++ {
		vouches[i].Value = !vouches[i].Value
		existingVouch = vouches[i].Idx
		_, err = sdb.UpdateVouch(existingVouch, vouches[i])
		require.NoError(t, err)
	}

	sdb.Close()
}

func TestScoreInStateDB(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	sdb, err := NewStateDB(Config{Path: dir, Keep: 128, Type: TypeSynchronizer, NLevels: 32})
	require.NoError(t, err)

	// create test scores
	var scores []*common.Score
	for i := 0; i < 4; i++ {
		scores = append(scores, newScore(i))
	}

	// get non-existing score, expecting an error
	unexistingScore := common.AccountIdx(1)
	_, err = sdb.GetScore(unexistingScore)
	assert.NotNil(t, err)
	assert.Equal(t, db.ErrNotFound, common.Unwrap(err))

	// add test scores
	for i := 0; i < len(scores); i++ {
		_, err = sdb.CreateScore(scores[i].Idx, scores[i])
		require.NoError(t, err)
	}

	for i := 0; i < len(scores); i++ {
		existingScore := scores[i].Idx
		scoGetted, err := sdb.GetScore(existingScore)
		require.NoError(t, err)
		assert.Equal(t, scores[i], scoGetted)
	}

	// try already existing idx and get error
	existingScore := common.AccountIdx(257)
	_, err = sdb.GetScore(existingScore) // check that exist
	require.NoError(t, err)
	_, err = sdb.CreateScore(common.AccountIdx(257), scores[1]) // check that can not be created twice
	assert.NotNil(t, err)
	assert.Equal(t, ErrScoreAlreadyExists, common.Unwrap(err))

	_, err = sdb.MTGetAccountProof(common.AccountIdx(257))
	require.NoError(t, err)

	// update scores
	for i := 0; i < len(scores); i++ {
		existingScore = scores[i].Idx
		_, err = sdb.UpdateScore(existingScore, scores[i])
		require.NoError(t, err)
	}

	sdb.Close()
}

func bigFromStr(h string, u int) *big.Int {
	if u == 16 {
		h = strings.TrimPrefix(h, "0x")
	}
	b, ok := new(big.Int).SetString(h, u)
	if !ok {
		panic("bigFromStr err")
	}
	return b
}

func TestCheckAccountsTreeTestVectors(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	sdb, err := NewStateDB(Config{Path: dir, Keep: 128, Type: TypeSynchronizer, NLevels: 32})
	require.NoError(t, err)

	ay0 := new(big.Int).Sub(new(big.Int).Exp(big.NewInt(2), big.NewInt(253), nil), big.NewInt(1))
	// test value from js version (compatibility-canary)
	assert.Equal(t, "1fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		(hex.EncodeToString(ay0.Bytes())))
	bjjPoint0Comp := babyjub.PackSignY(true, ay0)
	bjj0 := babyjub.PublicKeyComp(bjjPoint0Comp)

	ay1 := bigFromStr("00", 16)
	bjjPoint1Comp := babyjub.PackSignY(false, ay1)
	bjj1 := babyjub.PublicKeyComp(bjjPoint1Comp)
	ay2 := bigFromStr("21b0a1688b37f77b1d1d5539ec3b826db5ac78b2513f574a04c50a7d4f8246d7", 16)
	bjjPoint2Comp := babyjub.PackSignY(false, ay2)
	bjj2 := babyjub.PublicKeyComp(bjjPoint2Comp)

	ay3 := bigFromStr("0x10", 16) // 0x10=16
	bjjPoint3Comp := babyjub.PackSignY(false, ay3)
	require.NoError(t, err)
	bjj3 := babyjub.PublicKeyComp(bjjPoint3Comp)
	accounts := []*common.Account{
		{
			Idx:     1,
			BJJ:     bjj0,
			EthAddr: ethCommon.HexToAddress("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"),
			Nonce:   common.Nonce(0xFFFFFFFFFF),
			Balance: bigFromStr("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF", 16),
		},
		{
			Idx:     100,
			BJJ:     bjj1,
			EthAddr: ethCommon.HexToAddress("0x00"),
			Nonce:   common.Nonce(0),
			Balance: bigFromStr("0", 10),
		},
		{
			Idx:     0xFFFFFF,
			BJJ:     bjj2,
			EthAddr: ethCommon.HexToAddress("0xA3C88ac39A76789437AED31B9608da72e1bbfBF9"),
			Nonce:   common.Nonce(129),
			Balance: bigFromStr("42000000000000000000", 10),
		},
		{
			Idx:     10000,
			BJJ:     bjj3,
			EthAddr: ethCommon.HexToAddress("0x64"),
			Nonce:   common.Nonce(1900),
			Balance: bigFromStr("14000000000000000000", 10),
		},
	}
	for i := 0; i < len(accounts); i++ {
		_, err = accounts[i].HashValue()
		require.NoError(t, err)
		_, err = sdb.CreateAccount(accounts[i].Idx, accounts[i])
		if err != nil {
			log.Error(err)
		}
		require.NoError(t, err)
	}
	// root value generated by js version:
	assert.Equal(t,
		"6042732623065908215915192128670856333144563413308193635978934416138840158630",
		sdb.AccountTree.Root().BigInt().String())

	sdb.Close()
}

// // performActions function for Account and Link are to test the db setup and
// // it's mapping with merkel tree
// func performActionsAccount(a *models.Account, s *StateDB) {
// 	proof, err := s.PutAccount(a)
// 	if err != nil {
// 		log.Fatalf("Failed to store key-value pair: %v", err)
// 	}
// 	fmt.Println(proof, "----------------------- Circom Processor Proof ---------------------")

// 	// Retrieve and print a value
// 	value, err := s.GetAccount(a.Idx)
// 	if err != nil {
// 		log.Fatalf("Failed to retrieve value: %v", err)
// 	}
// 	fmt.Printf("Retrieved account: %+v\n", value)

// 	// Get and print root hash for leaf
// 	root := s.GetMTRoot(Account)
// 	fmt.Println(root, "MT root")
// }

// func performActionsLink(l *models.Link, s *StateDB) {
// 	proof, err := s.PutLink(l)
// 	if err != nil {
// 		log.Fatalf("Failed to store key-value pair: %v", err)
// 	}
// 	fmt.Println(proof, "----------------------- Circom Processor Proof ---------------------")
// 	// Retrieve and print a value
// 	value, err := s.GetLink(l.LinkIdx)
// 	if err != nil {
// 		log.Fatalf("Failed to retrieve value: %v", err)
// 	}
// 	fmt.Printf("Retrieved account: %+v\n", value)

// 	// Get and print root hash for leaf
// 	root := s.GetMTRoot(Link)
// 	fmt.Println(root, "MT root")
// }

// func printExamples(s *StateDB) {
// 	// Example accounts
// 	accountA := &models.Account{
// 		Idx:     1,
// 		EthAddr: "0xA",
// 		BJJ:     "ay_value",
// 		Balance: 10,
// 		Score:   1,
// 		Nonce:   0,
// 	}

// 	accountB := &models.Account{
// 		Idx:     2,
// 		EthAddr: "0xB",
// 		BJJ:     "ay_value",
// 		Balance: 10,
// 		Score:   1,
// 		Nonce:   0,
// 	}

// 	accountC := &models.Account{
// 		Idx:     3,
// 		EthAddr: "0xC",
// 		BJJ:     "ay_value",
// 		Balance: 10,
// 		Score:   1,
// 		Nonce:   0,
// 	}

// 	accountD := &models.Account{
// 		Idx:     4,
// 		EthAddr: "0xD",
// 		BJJ:     "ay_value",
// 		Balance: 10,
// 		Score:   1,
// 		Nonce:   0,
// 	}

// 	linkAB := &models.Link{
// 		LinkIdx: 11,
// 		Value:   true,
// 	}

// 	linkAC := &models.Link{
// 		LinkIdx: 13,
// 		Value:   true,
// 	}
// 	linkCD := &models.Link{
// 		LinkIdx: 34,
// 		Value:   true,
// 	}
// 	linkCA := &models.Link{
// 		LinkIdx: 31,
// 		Value:   true,
// 	}
// 	linkCB := &models.Link{
// 		LinkIdx: 32,
// 		Value:   true,
// 	}
// 	// Add Account A
// 	performActionsAccount(accountA, s)

// 	// Add Account B
// 	performActionsAccount(accountB, s)

// 	//Add Account C
// 	performActionsAccount(accountC, s)

// 	//Add Account D
// 	performActionsAccount(accountD, s)

// 	//Add Link AB
// 	performActionsLink(linkAB, s)

// 	performActionsLink(linkAC, s)
// 	performActionsLink(linkCD, s)
// 	performActionsLink(linkCA, s)
// 	performActionsLink(linkCB, s)

// 	// Print Merkle tree root
// 	// fmt.Printf("Merkle Account Tree Root: %s\n", s.AccountTree.Root.Hash)
// }

// func TestInitNewStateDB(t *testing.T) {
// 	dir, err := os.MkdirTemp("", "tmpdb")

// 	// Initialize the StateDB
// 	var stateDB *StateDB
// 	stateDB, err = NewStateDB(Config{Path: dir, Keep: 128, Type: TypeTxSelector, NLevels: 0})
// 	if err != nil {
// 		log.Fatalf("Failed to initialize StateDB: %v", err)
// 	}
// 	defer stateDB.Close()
// 	printExamples(stateDB)
// }

func TestNewStateDBIntermediateState(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	sdb, err := NewStateDB(Config{Path: dir, Keep: 128, Type: TypeTxSelector, NLevels: 0})
	require.NoError(t, err)

	// test values
	k0 := []byte("testkey0")
	k1 := []byte("testkey1")
	v0 := []byte("testvalue0")
	v1 := []byte("testvalue1")

	// store some data
	tx, err := sdb.db.DB().NewTx()
	require.NoError(t, err)
	err = tx.Put(k0, v0)
	require.NoError(t, err)
	err = tx.Commit()
	require.NoError(t, err)
	v, err := sdb.db.DB().Get(k0)
	require.NoError(t, err)
	assert.Equal(t, v0, v)

	// k0 not yet in last
	err = sdb.LastRead(func(sdb *Last) error {
		_, err := sdb.DB().Get(k0)
		assert.Equal(t, db.ErrNotFound, common.Unwrap(err))
		return nil
	})
	require.NoError(t, err)

	// Close PebbleDB before creating a new StateDB
	sdb.Close()

	// call NewStateDB which should get the db at the last checkpoint state
	// executing a Reset (discarding the last 'testkey0'&'testvalue0' data)
	sdb, err = NewStateDB(Config{Path: dir, Keep: 128, Type: TypeTxSelector, NLevels: 0})
	require.NoError(t, err)
	v, err = sdb.db.DB().Get(k0)
	assert.NotNil(t, err)
	assert.Equal(t, db.ErrNotFound, common.Unwrap(err))
	assert.Nil(t, v)

	// k0 not in last
	err = sdb.LastRead(func(sdb *Last) error {
		_, err := sdb.DB().Get(k0)
		assert.Equal(t, db.ErrNotFound, common.Unwrap(err))
		return nil
	})
	require.NoError(t, err)

	// store the same data from the beginning that has ben lost since last NewStateDB
	tx, err = sdb.db.DB().NewTx()
	require.NoError(t, err)
	err = tx.Put(k0, v0)
	require.NoError(t, err)
	err = tx.Commit()
	require.NoError(t, err)
	v, err = sdb.db.DB().Get(k0)
	require.NoError(t, err)
	assert.Equal(t, v0, v)

	// k0 yet not in last
	err = sdb.LastRead(func(sdb *Last) error {
		_, err := sdb.DB().Get(k0)
		assert.Equal(t, db.ErrNotFound, common.Unwrap(err))
		return nil
	})
	require.NoError(t, err)

	// make checkpoints with the current state
	bn, err := sdb.getCurrentBatch()
	require.NoError(t, err)
	assert.Equal(t, common.BatchNum(0), bn)
	err = sdb.MakeCheckpoint()
	require.NoError(t, err)
	bn, err = sdb.getCurrentBatch()
	require.NoError(t, err)
	assert.Equal(t, common.BatchNum(1), bn)

	// k0 in last
	err = sdb.LastRead(func(sdb *Last) error {
		v, err := sdb.DB().Get(k0)
		require.NoError(t, err)
		assert.Equal(t, v0, v)
		return nil
	})
	require.NoError(t, err)

	// write more data
	tx, err = sdb.db.DB().NewTx()
	require.NoError(t, err)
	err = tx.Put(k1, v1)
	require.NoError(t, err)
	err = tx.Put(k0, v1) // overwrite k0 with v1
	require.NoError(t, err)
	err = tx.Commit()
	require.NoError(t, err)

	v, err = sdb.db.DB().Get(k1)
	require.NoError(t, err)
	assert.Equal(t, v1, v)

	err = sdb.LastRead(func(sdb *Last) error {
		v, err := sdb.DB().Get(k0)
		require.NoError(t, err)
		assert.Equal(t, v0, v)
		return nil
	})
	require.NoError(t, err)

	// Close PebbleDB before creating a new StateDB
	sdb.Close()

	// call NewStateDB which should get the db at the last checkpoint state
	// executing a Reset (discarding the last 'testkey1'&'testvalue1' data)
	sdb, err = NewStateDB(Config{Path: dir, Keep: 128, Type: TypeTxSelector, NLevels: 0})
	require.NoError(t, err)

	bn, err = sdb.getCurrentBatch()
	require.NoError(t, err)
	assert.Equal(t, common.BatchNum(1), bn)

	// we closed the db without doing a checkpoint after overwriting k0, so
	// it's back to v0
	v, err = sdb.db.DB().Get(k0)
	require.NoError(t, err)
	assert.Equal(t, v0, v)

	v, err = sdb.db.DB().Get(k1)
	assert.NotNil(t, err)
	assert.Equal(t, db.ErrNotFound, common.Unwrap(err))
	assert.Nil(t, v)

	sdb.Close()
}

func TestStateDBGetAccounts(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	sdb, err := NewStateDB(Config{Path: dir, Keep: 128, Type: TypeTxSelector, NLevels: 0})
	require.NoError(t, err)

	// create test accounts
	var accounts []common.Account
	for i := 0; i < 16; i++ {
		account := newAccount(t, i)
		accounts = append(accounts, *account)
	}

	// add test accounts
	for i := range accounts {
		_, err = sdb.CreateAccount(accounts[i].Idx, &accounts[i])
		require.NoError(t, err)
	}

	dbAccounts, err := sdb.TestGetAccounts()
	require.NoError(t, err)
	assert.Equal(t, accounts, dbAccounts)

	sdb.Close()
}

// TestListCheckpoints performs almost the same test than kvdb/kvdb_test.go
// TestListCheckpoints, but over the StateDB
func TestListCheckpoints(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	sdb, err := NewStateDB(Config{Path: dir, Keep: 128, Type: TypeSynchronizer, NLevels: 32})
	require.NoError(t, err)

	numCheckpoints := 16
	// do checkpoints
	for i := 0; i < numCheckpoints; i++ {
		err = sdb.MakeCheckpoint()
		require.NoError(t, err)
	}
	list, err := sdb.db.ListCheckpoints()
	require.NoError(t, err)
	assert.Equal(t, numCheckpoints, len(list))
	assert.Equal(t, 1, list[0])
	assert.Equal(t, numCheckpoints, list[len(list)-1])

	numReset := 10
	err = sdb.Reset(common.BatchNum(numReset))
	require.NoError(t, err)
	list, err = sdb.db.ListCheckpoints()
	require.NoError(t, err)
	assert.Equal(t, numReset, len(list))
	assert.Equal(t, 1, list[0])
	assert.Equal(t, numReset, list[len(list)-1])

	sdb.Close()
}

// TestDeleteOldCheckpoints performs almost the same test than
// kvdb/kvdb_test.go TestDeleteOldCheckpoints, but over the StateDB
func TestDeleteOldCheckpoints(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	keep := 16
	sdb, err := NewStateDB(Config{Path: dir, Keep: keep, Type: TypeSynchronizer, NLevels: 32})
	require.NoError(t, err)

	numCheckpoints := 32
	// do checkpoints and check that we never have more than `keep`
	// checkpoints
	for i := 0; i < numCheckpoints; i++ {
		err = sdb.MakeCheckpoint()
		require.NoError(t, err)
		err := sdb.DeleteOldCheckpoints()
		require.NoError(t, err)
		checkpoints, err := sdb.db.ListCheckpoints()
		require.NoError(t, err)
		assert.LessOrEqual(t, len(checkpoints), keep)
	}

	sdb.Close()
}

// TestConcurrentDeleteOldCheckpoints performs almost the same test than
// kvdb/kvdb_test.go TestConcurrentDeleteOldCheckpoints, but over the StateDB
func TestConcurrentDeleteOldCheckpoints(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	keep := 16
	sdb, err := NewStateDB(Config{Path: dir, Keep: keep, Type: TypeSynchronizer, NLevels: 32})
	require.NoError(t, err)

	numCheckpoints := 32
	// do checkpoints and check that we never have more than `keep`
	// checkpoints
	for i := 0; i < numCheckpoints; i++ {
		err = sdb.MakeCheckpoint()
		require.NoError(t, err)
		wg := sync.WaitGroup{}
		n := 10
		wg.Add(n)
		for j := 0; j < n; j++ {
			go func() {
				err := sdb.DeleteOldCheckpoints()
				require.NoError(t, err)
				checkpoints, err := sdb.db.ListCheckpoints()
				require.NoError(t, err)
				assert.LessOrEqual(t, len(checkpoints), keep)
				wg.Done()
			}()
			_, err := sdb.db.ListCheckpoints()
			// only checking here for absence of errors, not the count of checkpoints
			require.NoError(t, err)
		}
		wg.Wait()
		checkpoints, err := sdb.db.ListCheckpoints()
		require.NoError(t, err)
		assert.LessOrEqual(t, len(checkpoints), keep)
	}

	sdb.Close()
}

func TestResetFromBadCheckpoint(t *testing.T) {
	dir, err := os.MkdirTemp("", "tmpdb")
	require.NoError(t, err)
	deleteme = append(deleteme, dir)

	keep := 16
	sdb, err := NewStateDB(Config{Path: dir, Keep: keep, Type: TypeSynchronizer, NLevels: 32})
	require.NoError(t, err)

	err = sdb.MakeCheckpoint()
	require.NoError(t, err)
	err = sdb.MakeCheckpoint()
	require.NoError(t, err)
	err = sdb.MakeCheckpoint()
	require.NoError(t, err)

	// reset from a checkpoint that doesn't exist
	err = sdb.Reset(10)
	require.Error(t, err)

	sdb.Close()
}
