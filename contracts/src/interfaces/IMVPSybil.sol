// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

interface IMVPSybil {
    error InvalidVerifierAddress();
    error LoadAmountExceedsLimit();
    error LoadAmountDoesNotMatch();
    error AmountExceedsLimit();
    error WithdrawAlreadyDone();
    error SmtProofInvalid();
    error EthTransferFailed();
    error InvalidProof();
    error InvalidFromIdx();
    error InvalidToIdx();

    // Initialization function
    function initialize(
        address verifier,
        uint256 maxTx,
        uint256 nLevel,
        address _poseidon2Elements,
        address _poseidon3Elements,
        address _poseidon4Elements
    ) external;

    // L1 Transaction functions
    function _addTx(
        address ethAddress,
        uint48 fromIdx,
        uint40 loadAmountF,
        uint40 amountF,
        uint48 toIdx
    ) external;

    // Batch forging function
    function forgeBatch(
        uint48 newLastIdx,
        uint256 newStRoot,
        uint256 newVouchRoot,
        uint256 newScoreRoot,
        uint256 newExitRoot,
        uint256[2] calldata proofA,
        uint256[2][2] calldata proofB,
        uint256[2] calldata proofC
    ) external;

    // Getter functions
    function getStateRoot(uint32 batchNum) external view returns (uint256);
    function getLastForgedBatch() external view returns (uint32);

    // L1 Transaction Queue functions
    function getL1TransactionQueue(
        uint32 queueIndex
    ) external view returns (bytes memory);
    function getQueueLength() external view returns (uint32);

    // Create the Account
    function createAccountDeposit(uint40 loadAmountF) external payable;

    // Deposit Function
    function deposit(uint48 fromIdx, uint40 loadAmountF) external payable;

    // Exit Function
    function exit(uint48 fromIdx, uint40 amountF) external;

    // Explode function
    function explodeMultiple(uint48 fromIdx, uint48[] memory toIdxs) external;

    // Vouch function
    function vouch(uint48 fromIdx, uint48 toIdx) external;

    // Unvouch function
    function unvouch(uint48 fromIdx, uint48 toIdx) external;

    function withdrawMerkleProof(
        uint192 amount,
        uint32 numExitRoot,
        uint256[] calldata siblings,
        uint48 idx
    ) external;

}
