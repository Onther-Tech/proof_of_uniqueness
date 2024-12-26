// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

import "forge-std/Test.sol";
import "../../src/mvp/Sybil.sol";
import "../../src/interfaces/IMVPSybil.sol";
import "../_helpers/constants.sol";
import "../_helpers/MVPTransactionTypes.sol";
import "../../src/stub/VerifierRollupStub.sol";

contract MvpTest is Test, TransactionTypeHelper {
    Sybil public sybil;
    bytes32[] public hashes;

    function setUp() public {
        PoseidonUnit2 mockPoseidon2 = new PoseidonUnit2();
        PoseidonUnit3 mockPoseidon3 = new PoseidonUnit3();
        PoseidonUnit4 mockPoseidon4 = new PoseidonUnit4();
        emit log_address(address(mockPoseidon2));
        emit log_address(address(mockPoseidon3));
        emit log_address(address(mockPoseidon4));

        VerifierRollupStub verifierStub = new VerifierRollupStub(); 

        address verifiers = address(verifierStub);
        uint256 maxTx = uint(256);
        uint256 nLevels = uint(1);

        sybil = new Sybil();

        sybil.initialize(
            verifiers, 
            maxTx, 
            nLevels, 
            address(mockPoseidon2), 
            address(mockPoseidon3), 
            address(mockPoseidon4)
        );
    }

    function testGetStateRoot() public {
        uint32 batchNum = 1;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            256, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );
        uint256 stateRoot = sybil.getStateRoot(batchNum);
        assertEq(stateRoot, 0xabc);
    }

    function testGetLastForgedBatch() public {
        uint32 lastForged = sybil.getLastForgedBatch();
        assertEq(lastForged, 0);

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            256, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        lastForged = sybil.getLastForgedBatch();
        assertEq(lastForged, 1);
    }

    function testGetL1TransactionQueue() public {
        TxParams memory params = validDeposit();

        uint256 loadAmount = _float2Fix(params.loadAmountF);
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            256, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        sybil.deposit {
            value: loadAmount
        }(params.fromIdx, params.loadAmountF);

        bytes memory txData = sybil.getL1TransactionQueue(1);
        bytes memory expectedTxData = abi.encodePacked(address(this), params.fromIdx, params.loadAmountF, params.amountF, params.toIdx);
        assertEq(txData, expectedTxData);
    }

    function testGetQueueLength() public {
        uint32 queueLength = sybil.getQueueLength();
        assertEq(queueLength, 1);

        TxParams memory params = validCreateAccountDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);

        vm.prank(address(this));
        sybil.createAccountDeposit {
            value: loadAmount
        }(params.loadAmountF);

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            256, 
            0xabc, 
            0, 
            0, 
            0,  
            proofA,
            proofB,
            proofC
        );

        queueLength = sybil.getQueueLength();
        assertEq(queueLength, 0);
    }

    function testClearQueue() public {
        TxParams memory params = validCreateAccountDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);

        vm.prank(address(this));
        sybil.createAccountDeposit {
            value: loadAmount
        }(params.loadAmountF);
    
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            256, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        uint32 queueAfter = sybil.getQueueLength();
        assertEq(queueAfter, 0);

        queueAfter = sybil.getQueueLength();
        assertEq(sybil.getLastForgedBatch(),1);
        assertEq(queueAfter, 0);
    }

    // Events tests
    function testForgeBatchEventEmission() public {
        vm.expectEmit(true, true, true, true);
        emit Sybil.ForgeBatch(1, 0);

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            256, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );
    }

    function testL1UserTxEventEmission() public {
        TxParams memory params = validCreateAccountDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);

        vm.expectEmit(true, true, true, true);
        emit Sybil.L1UserTxEvent(1, 0, abi.encodePacked(address(this), params.fromIdx, params.loadAmountF, params.amountF, params.toIdx));

        vm.prank(address(this));
        sybil.createAccountDeposit {
            value: loadAmount
        }(params.loadAmountF);
    }

    // CreateAccount transactions tests
    function testCreateDepositAccountTransaction() public {
        TxParams memory params = validCreateAccountDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);
        vm.prank(address(this));
        sybil.createAccountDeposit {
            value: loadAmount
        }(params.loadAmountF);
    }

    function testInvalidCreateDepositAccountTransaction() public {
        TxParams memory params = invalidCreateAccountDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.LoadAmountDoesNotMatch.selector);
        sybil.createAccountDeposit {
            value: 2*loadAmount
        }(params.loadAmountF);
    }

    function testInvalidCreateDepositAccountTransactionWithLoadAmountExceedsLimit() public {
        uint40 maxValue = 1099511627775;
        uint256 loadAmount = _float2Fix(maxValue);
        address addr = address(this);
        uint num = 34353197383670000000000000000000000000000000;
        vm.deal(addr, num);

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.LoadAmountExceedsLimit.selector);
        sybil.createAccountDeposit {
            value: loadAmount
        }(maxValue);
    }

    function testDepositTransaction() public {
        TxParams memory params = validDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        sybil.deposit {
            value: loadAmount
        }(params.fromIdx, params.loadAmountF);
    }

    function testInvalidDepositTransactionWithLoadAmountDoesNotMatch() public {
        TxParams memory params = validDeposit();
        uint256 loadAmount = (params.loadAmountF) * 10 ** (18 - 8);

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.LoadAmountDoesNotMatch.selector);
        sybil.deposit {
            value: 2*loadAmount
        }(params.fromIdx, params.loadAmountF);
    }

    function testInvalidDepositTransactionWithInvalidFromIdx() public {
        TxParams memory params = invalidDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);

        uint48 initialLastIdx = 255;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidFromIdx.selector);
        sybil.deposit {
            value: loadAmount
        }(params.fromIdx, params.loadAmountF);
    }

    function testInvalidDepositTransactionWithLoadAmountExceedsLimit() public {
        TxParams memory params = validDeposit();
        uint40 maxValue = 1099511627775;
        uint256 loadAmount = _float2Fix(maxValue);
        address addr = address(this);
        uint num = 34353197383670000000000000000000000000000000;
        vm.deal(addr, num);

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.LoadAmountExceedsLimit.selector);
        sybil.deposit {
            value: loadAmount
        }(params.fromIdx, maxValue);
    }

    function testVouch() public {
        TxParams memory params = validVouch();

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));

        // forging to set the lastIdx
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        sybil.vouch(params.fromIdx, params.toIdx);
    }

        function testInvalidVouchWithInvalidFromIdx() public {
        TxParams memory params = validVouch();

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        // forging to set the lastIdx
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidFromIdx.selector);
        // fromIdx is 244 due to which the tx reverts
        sybil.vouch(244, params.toIdx);
    }

    function testInvalidVouchWithInvalidToIdx() public {
        TxParams memory params = validVouch();

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        // forging to set the lastIdx
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0,
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidToIdx.selector);
        // toIdx is 244 due to which the tx reverts
        sybil.vouch(params.fromIdx, 244);
    }


    function testUnVouch() public {
        TxParams memory params = validUnVouch();

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        // forging to set the lastIdx
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0,
            0, 
            proofA,
            proofB,
            proofC
        );
    
        vm.prank(address(this));
        sybil.unvouch(params.fromIdx, params.toIdx);
    }

        function testInvalidUnVouchWitInvalidFromIdx() public {
        TxParams memory params = validUnVouch();

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        // forging to set the lastIdx
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidFromIdx.selector);
        // fromIdx is 244 due to which the tx reverts
        sybil.unvouch(244, params.toIdx);
    }

    function testInvalidUnVouchWitInvalidToIdx() public {
        TxParams memory params = validUnVouch();

        uint48 initialLastIdx = 256;
        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        // forging to set the lastIdx
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidToIdx.selector);
        // toIdx is 244 due to which the tx reverts
        sybil.unvouch(params.fromIdx, 244);
    }

    // ForceExit transactions tests
    function testForceExitTransaction() public {
        TxParams memory params = validForceExit();
        uint48 initialLastIdx = 256;

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        sybil.exit(params.fromIdx, params.amountF);
    }

    function testInvalidForceExitTransactionWithInvalidFromIdx() public {
        TxParams memory params = invalidForceExit();
        uint48 initialLastIdx = 256;

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidFromIdx.selector);
        sybil.exit(params.fromIdx, params.amountF);
    }

    function testInvalidForceExitTransactionWithAmountExceedsLimit() public {
        uint48 initialLastIdx = 256;

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );
        TxParams memory params = validForceExit();
        uint40 maxValue = 1099511627775;

        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.AmountExceedsLimit.selector);
        sybil.exit(params.fromIdx, maxValue);
    }

    // ForceExplode transactions tests
    function testExplodeMultiple() public {
        TxParams memory params = validForceExplode();
        uint48 initialLastIdx = 256;

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        uint48 fromIdx = params.fromIdx;

        uint48[] memory toIdxs = new uint48[](1);
        toIdxs[0] = params.toIdx;
        vm.prank(address(this));
        sybil.explodeMultiple(fromIdx, toIdxs);
        
    }

    function testExplodeMultipleWithInvalidToIdx() public {
        TxParams memory params = invalidFromIdxForceExplode();
        uint48 initialLastIdx = 256;

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0, 
            proofA,
            proofB,
            proofC
        );

        uint48 fromIdx = params.fromIdx;

        uint48[] memory toIdxs = new uint48[](1);
        toIdxs[0] = params.toIdx;
        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidFromIdx.selector);
        sybil.explodeMultiple(fromIdx, toIdxs); 
    }

            function testExplodeMultipleWithInvalidFromIdx() public {
        TxParams memory params = invalidToIdxForceExplode();
        uint48 initialLastIdx = 256;

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];

        vm.prank(address(this));
        sybil.forgeBatch(
            initialLastIdx, 
            0xabc, 
            0, 
            0, 
            0,
            proofA,
            proofB,
            proofC
        );

        uint48 fromIdx = params.fromIdx;

        uint48[] memory toIdxs = new uint48[](1);
        toIdxs[0] = params.toIdx;
        vm.prank(address(this));
        vm.expectRevert(IMVPSybil.InvalidToIdx.selector);
        sybil.explodeMultiple(fromIdx, toIdxs); 
    }

    function testInitializeWithInvalidPoseidonAddresses() public {
        PoseidonUnit2 mockPoseidon2 = new PoseidonUnit2();
        PoseidonUnit3 mockPoseidon3 = new PoseidonUnit3();
        PoseidonUnit4 mockPoseidon4 = new PoseidonUnit4();
        // Deploy verifier stub
        VerifierRollupStub verifierStub = new VerifierRollupStub(); 
        
        address verifiers = address(verifierStub);
        uint256 maxTx = uint(256);
        uint256 nLevels = uint(1);

        address invalidAddress = address(0);

        // Expect revert for invalid poseidon2Elements address
        Sybil newSybil = new Sybil();
        vm.expectRevert();
        newSybil.initialize(
            verifiers, 
            maxTx, 
            nLevels, 
            invalidAddress, 
            address(mockPoseidon3), 
            address(mockPoseidon4)
        );

        // Expect revert for invalid poseidon3Elements address
        vm.expectRevert();
        newSybil.initialize(
            verifiers, 
            maxTx, 
            nLevels, 
            address(mockPoseidon2), 
            invalidAddress, 
            address(mockPoseidon4)
        );

        // Expect revert for invalid poseidon4Elements address
        vm.expectRevert();
        newSybil.initialize(
            verifiers, 
            maxTx, 
            nLevels, 
            address(mockPoseidon2), 
            address(mockPoseidon3),
            invalidAddress
        );
    }

        // Test initializing with invalid verifier address
    function testInitializeWithInvalidVerifierAddresses() public {
        PoseidonUnit2 mockPoseidon2 = new PoseidonUnit2();
        PoseidonUnit3 mockPoseidon3 = new PoseidonUnit3();
        PoseidonUnit4 mockPoseidon4 = new PoseidonUnit4();
        
        address verifier = address(0);
        uint256 maxTx = uint(256);
        uint256 nLevel = uint(1);

        // Expect revert for invalid verifier address
        Sybil newSybil = new Sybil();
        vm.expectRevert(IMVPSybil.InvalidVerifierAddress.selector);
        newSybil.initialize(
            verifier, 
            maxTx, 
            nLevel, 
            address(mockPoseidon2), 
            address(mockPoseidon3), 
            address(mockPoseidon4)
        );
    }

        function testWithdrawMerkleProofTransferFails() public {
        uint192 amount = 1 ether;
        uint32 numExitRoot = 1;
        uint48 idx = 0;

        uint256 [] memory siblings; // Empty siblings

        // Expect revert ETH transfer failed due to Sybil contract doesn't have enough ether to send
        vm.expectRevert(IMVPSybil.EthTransferFailed.selector);
        sybil.withdrawMerkleProof(
            amount,
            numExitRoot,
            siblings,
            idx
        );
    }

    function testWithdrawMerkleProofAlreadyDone() public {
        uint32 numExitRoot = 1;
        uint48 idx = 0;

        uint256 [] memory siblings; 

        TxParams memory params = validCreateAccountDeposit();
        uint256 loadAmount = _float2Fix(params.loadAmountF);
        vm.prank(address(this));
        sybil.createAccountDeposit {
            value: loadAmount
        }(params.loadAmountF);

        vm.prank(address(this));
        // Withdraw for the first time
        sybil.withdrawMerkleProof(
            uint192(loadAmount),
            numExitRoot,
            siblings,
            idx
        );

        vm.expectRevert(IMVPSybil.WithdrawAlreadyDone.selector);
        // Reverts as Withdraw Already Done
        sybil.withdrawMerkleProof(
            uint192(loadAmount),
            numExitRoot,
            siblings,
            idx
        );
    }

    function testWithdrawMerkleProofTransferPasses() public {
        uint32 numExitRoot = 1;
        uint48 idx = 2;
        
        // Calcuate exit root
        bytes32 exitRoot = calculateTestExitTreeRoot();

        TxParams memory params = validCreateAccountDeposit();     
        uint256 loadAmount = _float2Fix(params.loadAmountF);

        vm.prank(address(this));
        sybil.createAccountDeposit {
            value: loadAmount
        }(params.loadAmountF);

        uint256[2] memory proofA = [uint(0),uint(0)];
        uint256[2][2] memory proofB = [[uint(0), uint(0)], [uint(0), uint(0)]];
        uint256[2] memory proofC = [uint(0), uint(0)];
        
        vm.prank(address(this));
        sybil.forgeBatch(
            256, 
            0xabc, 
            0, 
            0, 
            uint(exitRoot), 
            proofA,
            proofB,
            proofC
        );

        /* verify
            3rd leaf
            0xdca3326ad7e8121bf9cf9c12333e6b2271abe823ec9edfe42f813b1e768fa57b

            root
            0xcc086fcc038189b4641db2cc4f1de3bb132aefbd65d510d817591550937818c7

            index
            2

            proof
            0x8da9e1c820f9dbd1589fd6585872bc1063588625729e7ab0797cfc63a00bd950
            0x995788ffc103b987ad50f5e5707fd094419eb12d9552cc423bd0cd86a3861433
        */
        // Calculate proof (sibling)
        uint[] memory siblings = new uint[](2);
        siblings[0] = uint(0x8da9e1c820f9dbd1589fd6585872bc1063588625729e7ab0797cfc63a00bd950);
        siblings[1] = uint(0x995788ffc103b987ad50f5e5707fd094419eb12d9552cc423bd0cd86a3861433);

        bytes32 leaf = bytes32(0xdca3326ad7e8121bf9cf9c12333e6b2271abe823ec9edfe42f813b1e768fa57b);

        // verify proof
        bool isVerified = verify(
            siblings,
            exitRoot,
            leaf,
            idx
        );

        assert(isVerified == true);
        uint256 balanceBefore = address(this).balance;
        sybil.withdrawMerkleProof(
            uint192(loadAmount),
            numExitRoot,
            siblings,
            idx
        );   
        // loadAmount is transferred to this contract by Sybil.sol
        assertEq(address(this).balance, balanceBefore + loadAmount);
    }

    function calculateTestExitTreeRoot() internal returns (bytes32) {
        uint256[4] memory transactions = [uint(0), uint(1), uint(2), uint(3)];
        uint256[4] memory keys = [uint(0), uint(1), uint(2), uint(3)];

        for (uint256 i = 0; i < transactions.length; i++) {
            uint256 hashValue = sybil._hashNode(keys[i], transactions[i]);
            hashes.push(bytes32(hashValue));
        }

        uint256 n = transactions.length;
        uint256 offset = 0;

        while (n > 0) {
            for (uint256 i = 0; i < n - 1; i += 2) {
                uint256 res = sybil._hashNode(uint(hashes[offset + i]), uint(hashes[offset + i + 1]));
                hashes.push(
                    bytes32(res)
                );
            }
            offset += n;
            n = n / 2;
        }

        return hashes[hashes.length - 1];
    }

    function verify(
        uint[] memory proof,
        bytes32 root,
        bytes32 leaf,
        uint256 index
    ) internal view returns (bool) {
        uint256 hash = uint(leaf);

        for (uint256 i = 0; i < proof.length; i++) {
            uint256 proofElement = uint(proof[i]);

            if (index % 2 == 0) {
                hash = sybil._hashNode(hash, proofElement);
            } else {
                hash = sybil._hashNode(proofElement, hash);
            }

            index = index / 2;
        }

        return hash == uint(root);
    }

    function _float2Fix(uint40 floatVal) internal pure returns(uint256) {
        uint256 m = floatVal & 0x7FFFFFFFF;
        uint256 e = floatVal >> 35;

        uint256 exp = 10**e;
        uint256 fix = m * exp;

        return fix;
    }

    receive() external payable { }
}
