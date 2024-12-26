package common

import (
	"time"

	ethCommon "github.com/ethereum/go-ethereum/common"
	ethMath "github.com/ethereum/go-ethereum/common/math"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
	ethSigner "github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/iden3/go-iden3-crypto/babyjub"
)

// AccountCreationAuth authorizations sent by users to the L2DB, to be used for
// account creations when necessary
type AccountCreationAuth struct {
	EthAddr   ethCommon.Address     `meddler:"eth_addr"`
	BJJ       babyjub.PublicKeyComp `meddler:"bjj"`
	Signature []byte                `meddler:"signature"`
	Timestamp time.Time             `meddler:"timestamp,utctime"`
}

const (
	// AccountCreationAuthMsg is the message that is signed to authorize a
	// Hermez account creation
	AccountCreationAuthMsg = "Account creation"
	// EIP712Version is the used version of the EIP-712
	EIP712Version = "1"
	// EIP712Provider defines the Provider for the EIP-712
	EIP712Provider = "Tokamak Network"
)

// toHash returns a byte array to be hashed from the AccountCreationAuth, which
// follows the EIP-712 encoding
func (a *AccountCreationAuth) toHash(chainID uint16,
	hermezContractAddr ethCommon.Address) ([]byte, error) {
	chainIDFormatted := ethMath.NewHexOrDecimal256(int64(chainID))

	signerData := ethSigner.TypedData{
		Types: ethSigner.Types{
			"EIP712Domain": []ethSigner.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Authorise": []ethSigner.Type{
				{Name: "Provider", Type: "string"},
				{Name: "Authorisation", Type: "string"},
				{Name: "BJJKey", Type: "bytes32"},
			},
		},
		PrimaryType: "Authorise",
		Domain: ethSigner.TypedDataDomain{
			Name:              EIP712Provider,
			Version:           EIP712Version,
			ChainId:           chainIDFormatted,
			VerifyingContract: hermezContractAddr.Hex(),
		},
		Message: ethSigner.TypedDataMessage{
			"Provider":      EIP712Provider,
			"Authorisation": AccountCreationAuthMsg,
			"BJJKey":        SwapEndianness(a.BJJ[:]),
		},
	}

	domainSeparator, err := signerData.HashStruct("EIP712Domain", signerData.Domain.Map())
	if err != nil {
		return nil, Wrap(err)
	}
	typedDataHash, err := signerData.HashStruct(signerData.PrimaryType, signerData.Message)
	if err != nil {
		return nil, Wrap(err)
	}

	rawData := []byte{0x19, 0x01} // "\x19\x01"
	rawData = append(rawData, domainSeparator...)
	rawData = append(rawData, typedDataHash...)
	return rawData, nil
}

// HashToSign returns the hash to be signed by the Ethereum address to authorize
// the account creation, which follows the EIP-712 encoding
func (a *AccountCreationAuth) HashToSign(chainID uint16,
	hermezContractAddr ethCommon.Address) ([]byte, error) {
	b, err := a.toHash(chainID, hermezContractAddr)
	if err != nil {
		return nil, Wrap(err)
	}
	return ethCrypto.Keccak256(b), nil
}

// Sign signs the account creation authorization message using the provided
// `signHash` function, and stores the signature in `a.Signature`.  `signHash`
// should do an ethereum signature using the account corresponding to
// `a.EthAddr`.  The `signHash` function is used to make signing flexible: in
// tests we sign directly using the private key, outside tests we sign using
// the keystore (which never exposes the private key). Sign follows the EIP-712
// encoding.
func (a *AccountCreationAuth) Sign(signHash func(hash []byte) ([]byte, error),
	chainID uint16, hermezContractAddr ethCommon.Address) error {
	hash, err := a.HashToSign(chainID, hermezContractAddr)
	if err != nil {
		return Wrap(err)
	}
	sig, err := signHash(hash)
	if err != nil {
		return Wrap(err)
	}
	sig[64] += 27
	a.Signature = sig
	a.Timestamp = time.Now()
	return nil
}
