// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

import "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import "../interfaces/IMVPSybil.sol";
import "../interfaces/IVerifierRollup.sol";
import "./SybilHelpers.sol";

contract Sybil is Initializable, OwnableUpgradeable, IMVPSybil, MVPSybilHelpers {
    uint48 constant _RESERVED_IDX = 255;
    uint48 constant _EXIT_IDX = 1;
    uint48 constant _EXPLODE_IDX = 2;
    uint256 constant _TXN_TOTALBYTES = 128; // Total bytes per transaction
    uint256 constant _MAX_TXNS = 1000; // Max transactions per batch
    uint256 constant _LIMIT_LOADAMOUNT = (1 << 128); // Max loadAmount per call
    uint256 constant _RFIELD = 21888242871839275222246405745257275088548364400416034343698204186575808495617;

    uint48 public lastIdx;
    uint32 public lastForgedBatch;
    uint32 public currentFillingBatch;

    mapping(uint32 => uint256) public accountRootMap;
    mapping(uint32 => uint256) public vouchRootMap;
    mapping(uint32 => uint256) public scoreRootMap;
    mapping(uint32 => uint256) public exitRootMap;
    mapping(uint32 => bytes) public unprocessedBatchesMap;

    // Mapping of exit nullifiers, only allowing each withdrawal to be made once
    mapping(uint32 => mapping(uint48 => bool)) public exitNullifierMap;

    struct VerifierRollup {
        VerifierRollupInterface verifierInterface;
        uint256 maxTx; // maximum rollup transactions in a batch: L1-tx transactions
        uint256 nLevel; // number of levels of the circuit
    }

    // Verifier
    VerifierRollup public rollupVerifier;

    event L1UserTxEvent(
        uint32 indexed queueIndex,
        uint8 indexed position,
        bytes l1UserTx
    );

    event ForgeBatch(uint32 indexed batchNum, uint16 l1UserTxsLen);
    event WithdrawEvent(
        uint48 indexed idx,
        uint32 indexed numExitRoot
    );

    function initialize(
        address verifier,
        uint256 maxTx,
        uint256 nLevel,
        address _poseidon2Elements,
        address _poseidon3Elements,
        address _poseidon4Elements
    ) public initializer {
        lastIdx = _RESERVED_IDX;
        currentFillingBatch = 1;

        _initializeVerifiers(
            verifier,
            maxTx,
            nLevel
        );

        _initializeHelpers(
            _poseidon2Elements,
            _poseidon3Elements,
            _poseidon4Elements
        );
    }

    function _addTx(
        address ethAddress,
        uint48 fromIdx,
        uint40 loadAmountF,
        uint40 amountF,
        uint48 toIdx
    ) public override {
        bytes memory l1Tx = abi.encodePacked(
            ethAddress,
            fromIdx,
            loadAmountF,
            amountF,
            toIdx
        );

        uint256 currentPosition = unprocessedBatchesMap[currentFillingBatch].length /
            _TXN_TOTALBYTES;

        unprocessedBatchesMap[currentFillingBatch] = bytes.concat(
            unprocessedBatchesMap[currentFillingBatch],
            l1Tx
        );

        emit L1UserTxEvent(currentFillingBatch, uint8(currentPosition), l1Tx);

        if (currentPosition + 1 >= _MAX_TXNS) {
            currentFillingBatch++;
        }
    }

    function createAccountDeposit(uint40 loadAmountF) external payable override {
        uint256 loadAmount = _float2Fix(loadAmountF);
        if(loadAmount >= _LIMIT_LOADAMOUNT) {
            revert LoadAmountExceedsLimit();
        }

        if(loadAmount != msg.value) {
            revert LoadAmountDoesNotMatch();
        }
        _addTx(msg.sender, 0, loadAmountF, 0, 0);
    }

    function deposit(uint48 fromIdx, uint40 loadAmountF) external payable override {
        uint256 loadAmount = _float2Fix(loadAmountF);

        if(loadAmount >= _LIMIT_LOADAMOUNT) {
            revert LoadAmountExceedsLimit();
        }

        if(loadAmount != msg.value) {
            revert LoadAmountDoesNotMatch();
        }

        if((fromIdx <= _RESERVED_IDX) || (fromIdx > lastIdx)) {
            revert InvalidFromIdx();
        }

        _addTx(msg.sender, fromIdx, loadAmountF, 0, 0);
    }


    function exit(uint48 fromIdx, uint40 amountF) external override {
        uint256 amount = _float2Fix(amountF);

        if(amount >= _LIMIT_LOADAMOUNT) {
            revert AmountExceedsLimit();
        }

        if((fromIdx <= _RESERVED_IDX) || (fromIdx > lastIdx)) {
            revert InvalidFromIdx();
        }

        _addTx(msg.sender, fromIdx, 0, amountF, _EXIT_IDX);
    }

    function explodeMultiple(uint48 fromIdx, uint48[] memory toIdxs) external override {

        if((fromIdx <= _RESERVED_IDX) || (fromIdx > lastIdx)) {
            revert InvalidFromIdx();
        }
        uint256 length = toIdxs.length;
        for (uint256 i = 0; i < length; ++i) {
            if((toIdxs[i] <= _RESERVED_IDX) || (toIdxs[i] > lastIdx)) {
                revert InvalidToIdx();
            }
        }

        for (uint256 i = 0; i < length; ++i) {
            _addTx(msg.sender, fromIdx, 0, 2, toIdxs[i]);
        }
    }

    function vouch(uint48 fromIdx, uint48 toIdx) external {

        if((fromIdx <= _RESERVED_IDX) || (fromIdx > lastIdx)) {
            revert InvalidFromIdx();
        }

        if(((toIdx <= _RESERVED_IDX) || (toIdx > lastIdx))) {
                revert InvalidToIdx();
        }

        _addTx(msg.sender, fromIdx, 0, 1, toIdx);
    }

    function unvouch(uint48 fromIdx, uint48 toIdx) external {

        if((fromIdx <= _RESERVED_IDX) || (fromIdx > lastIdx)) {
            revert InvalidFromIdx();
        }

        if(((toIdx <= _RESERVED_IDX) || (toIdx > lastIdx))) {
                revert InvalidToIdx();
        }

        _addTx(msg.sender, fromIdx, 0, 0, toIdx);
    }

    // Implement the missing function from the IMvp interface
    function forgeBatch(
        uint48 newLastIdx,
        uint256 newAccountRoot,
        uint256 newVouchRoot,
        uint256 newScoreRoot,
        uint256 newExitRoot,
        uint256[2] calldata proofA,
        uint256[2][2] calldata proofB,
        uint256[2] calldata proofC
    ) external override {
        uint256 input = _constructCircuitInput(
          newLastIdx,
          newAccountRoot,
          newVouchRoot,
          newScoreRoot,
          newExitRoot
      );

        // Verify the proof using the specific rollup verifier
        if (
            !rollupVerifier.verifierInterface.verifyProof(
                proofA,
                proofB,
                proofC,
                [input]
            )
        ) {
            revert InvalidProof();
        }

        lastForgedBatch++;
        lastIdx = newLastIdx;
        accountRootMap[lastForgedBatch] = newAccountRoot;
        vouchRootMap[lastForgedBatch] = newVouchRoot;
        scoreRootMap[lastForgedBatch] = newScoreRoot;
        exitRootMap[lastForgedBatch] = newExitRoot;

        uint16 l1UserTxsLen = _clearBatchFromQueue();

        emit ForgeBatch(lastForgedBatch, l1UserTxsLen);
    }

    function _constructCircuitInput(
        uint48 newLastIdx,
        uint256 newAccountRoot,
        uint256 newVouchRoot,
        uint256 newScoreRoot,
        uint256 newExitRoot
    ) internal view returns (uint256) {
        uint256 oldAccountRoot = accountRootMap[lastForgedBatch];
        uint256 oldVouchRoot = vouchRootMap[lastForgedBatch];
        uint256 oldScoreRoot = scoreRootMap[lastForgedBatch];
        uint48 oldLastIdx = lastIdx;
        bytes memory txnData = unprocessedBatchesMap[lastForgedBatch+1];

        bytes memory inputBytes = abi.encodePacked(
            oldLastIdx,
            oldAccountRoot,
            oldVouchRoot,
            oldScoreRoot,
            newLastIdx,
            newAccountRoot,
            newVouchRoot,
            newScoreRoot,
            newExitRoot,
            txnData
        );
        return uint256(sha256(inputBytes)) % _RFIELD;
}    

    function _clearBatchFromQueue() internal returns (uint16) {
        uint16 l1UserTxsLen = uint16(
            unprocessedBatchesMap[lastForgedBatch].length / _TXN_TOTALBYTES
        );
        delete unprocessedBatchesMap[lastForgedBatch];
        if (lastForgedBatch + 1 == currentFillingBatch) {
            currentFillingBatch++;
        }
        return l1UserTxsLen;
    }

    function withdrawMerkleProof(
        uint192 amount,
        uint32 numExitRoot,
        uint256[] calldata siblings,
        uint48 idx
    ) external {
        uint256[4] memory arrayState = _buildTreeState(amount, msg.sender);
        uint256 stateHash = _hash4Elements(arrayState);

        uint256 exitRoot = exitRootMap[numExitRoot];

        if (exitNullifierMap[numExitRoot][idx]) {
            revert WithdrawAlreadyDone();
        }

        if (!_smtVerifier(exitRoot, siblings, idx, stateHash)) {
            revert SmtProofInvalid();
        }

        exitNullifierMap[numExitRoot][idx] = true;

        _withdrawFunds(amount);
        emit WithdrawEvent(idx, numExitRoot);
    }

    function _buildTreeState(uint192 amount, address user) internal pure returns (uint256[4] memory) {
        uint256[4] memory state;
        state[0] = amount;
        state[1] = uint256(uint160(user)); // Convert address to uint256
        state[2] = 0;
        state[3] = 0;
        return state;
    }

    function _withdrawFunds(uint192 amount) internal {
        _safeTransfer(amount);
    }

    function _safeTransfer(uint256 value) internal {
        (bool success, ) = msg.sender.call{value: value}(new bytes(0));
        if (!success) {
            revert EthTransferFailed();
        }
    }

    function getStateRoot(uint32 batchNum) external view override returns (uint256) {
        return accountRootMap[batchNum];
    }

    function getLastForgedBatch() external view override returns (uint32) {
        return lastForgedBatch;
    }

    function getL1TransactionQueue(uint32 queueIndex) external view override returns (bytes memory) {
        return unprocessedBatchesMap[queueIndex];
    }

    function getQueueLength() external view override returns (uint32) {
        return currentFillingBatch - lastForgedBatch;
    }

    function _float2Fix(uint40 floatVal) internal pure returns(uint256) {
        uint256 m = floatVal & 0x7FFFFFFFF;
        uint256 e = floatVal >> 35;

        uint256 exp = 10**e;
        uint256 fix = m * exp;

        return fix;
    }

    function _initializeVerifiers(
        address _verifier,
        uint256 _maxTx,
        uint256 _nLevel
    ) internal {
        if (_verifier == address(0)) {
            revert InvalidVerifierAddress();
        }

        rollupVerifier = VerifierRollup({
            verifierInterface: VerifierRollupInterface(_verifier),
            maxTx: _maxTx,
            nLevel: _nLevel
        });
    }
}
