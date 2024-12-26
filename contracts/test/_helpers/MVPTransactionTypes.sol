// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

contract TransactionTypeHelper {
    struct TxParams {
        uint48 fromIdx;
        uint40 loadAmountF;
        uint40 amountF;
        uint48 toIdx;
    }


    // Returns valid deposit transaction parameters
    function validDeposit() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 256, 
            loadAmountF: 100, 
            amountF: 0, 
            toIdx: 0
        });
    }

    // Returns invalid deposit transaction parameters
    function invalidDeposit() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 255, 
            loadAmountF: 100, 
            amountF: 0, 
            toIdx: 0
        });
    }

    // Returns valid CreateAccount transaction parameters
    function validCreateAccountDeposit() public pure returns (TxParams memory) {
        return TxParams({ 
            fromIdx: 0, 
            loadAmountF: 100, 
            amountF: 0, 
            toIdx: 0
        });
    }

    // Returns invalid CreateAccount transaction parameters
    function invalidCreateAccountDeposit() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 0, 
            loadAmountF: 100, 
            amountF: 0, 
            toIdx: 0
        });
    }

    // Returns valid ForceExit transaction parameters
    function validForceExit() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 256, 
            loadAmountF: 0, 
            amountF: 0, 
            toIdx: 1 
        });
    }

    // Returns invalid ForceExit transaction parameters
    function invalidForceExit() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 255, 
            loadAmountF: 0,
            amountF: 0, 
            toIdx: 1 
        });
    }

    // Returns valid ForceExplode transaction parameters
    function validForceExplode() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 256, 
            loadAmountF: 0, 
            amountF: 0, 
            toIdx: 256
        });
    }

    // Returns invalid ForceExplode transaction parameters
    function invalidFromIdxForceExplode() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 255, 
            loadAmountF: 0, 
            amountF: 0, 
            toIdx: 256
        });
    }

    // Returns invalid ForceExplode transaction parameters
    function invalidToIdxForceExplode() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 256, 
            loadAmountF: 0, 
            amountF: 0, 
            toIdx: 255
        });
    }

    // Returns valid Vouch transaction parameters
    function validVouch() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 256, 
            loadAmountF: 0, 
            amountF: 1, 
            toIdx: 256
        });
    }

    // Returns Invalid Vouch transaction parameters
    function validUnVouch() public pure returns (TxParams memory) {
        return TxParams({
            fromIdx: 256, 
            loadAmountF: 0, 
            amountF: 1, 
            toIdx: 256
        });
    }
}
