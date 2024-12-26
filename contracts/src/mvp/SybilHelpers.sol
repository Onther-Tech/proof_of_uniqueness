// SPDX-License-Identifier: AGPL-3.0
pragma solidity 0.8.23;

error InvalidPoseidonAddress(string elementType);

/**
 * @dev Interface poseidon hash function 2 elements
 */
contract PoseidonUnit2 {
    function poseidon(uint256[2] memory) public pure returns(uint256) {}
}

/**
 * @dev Interface poseidon hash function 3 elements
 */
contract PoseidonUnit3 {
    function poseidon(uint256[3] memory) public pure returns(uint256) {}
}

/**
 * @dev Interface poseidon hash function 4 elements
 */
contract PoseidonUnit4 {
    function poseidon(uint256[4] memory) public pure returns(uint256) {}
}

/**
 * @dev Sybil helper functions
 */
contract MVPSybilHelpers {
    PoseidonUnit2 _insPoseidonUnit2;
    PoseidonUnit3 _insPoseidonUnit3;
    PoseidonUnit4 _insPoseidonUnit4;

    /**
     * @dev Load poseidon smart contract
     * @param _poseidon4Elements Poseidon contract address for 4 elements
     */
 function _initializeHelpers(
    address _poseidon2Elements,
    address _poseidon3Elements,
    address _poseidon4Elements
) internal {
    if (_poseidon2Elements == address(0)) {
        revert InvalidPoseidonAddress("poseidon2Elements");
    }
    if (_poseidon3Elements == address(0)) {
        revert InvalidPoseidonAddress("poseidon3Elements");
    }
    if (_poseidon4Elements == address(0)) {
        revert InvalidPoseidonAddress("poseidon4Elements");
    }

    _insPoseidonUnit2 = PoseidonUnit2(_poseidon2Elements);
    _insPoseidonUnit3 = PoseidonUnit3(_poseidon3Elements);
    _insPoseidonUnit4 = PoseidonUnit4(_poseidon4Elements);
}


    /**
     * @dev Build entry for the exit tree leaf
     * @param nonce nonce parameter, only use 40 bits instead of 48
     * @param balance Balance of the account
     * @param ay Public key babyjubjub represented as point: sign + (Ay)
     * @param ethAddress Ethereum address
     * @return uint256 array with the state variables
     */
    function _buildTreeState(
        uint48 nonce,
        uint256 balance,
        uint256 ay,
        address ethAddress
    ) internal pure returns(uint256[4] memory) {
        uint256[4] memory stateArray;

        stateArray[0] |= nonce << 32;
        stateArray[0] |= (ay >> 255) << (32 + 40);
        // build element 2
        stateArray[1] = balance;
        // build element 4
        stateArray[2] = (ay << 1) >> 1; // last bit set to 0
        // build element 5
        stateArray[3] = uint256(uint160(ethAddress));
        return stateArray;
    }

    /**
     * @dev Hash poseidon for 2 elements
     * @param inputs Poseidon input array of 2 elements
     * @return Poseidon hash
     */
    function _hash2Elements(uint256[2] memory inputs)
    internal
    view
    returns(uint256) {
        return _insPoseidonUnit2.poseidon(inputs);
    }

    /**
     * @dev Hash poseidon for 3 elements
     * @param inputs Poseidon input array of 3 elements
     * @return Poseidon hash
     */
    function _hash3Elements(uint256[3] memory inputs)
    internal
    view
    returns(uint256) {
        return _insPoseidonUnit3.poseidon(inputs);
    }

    /**
     * @dev Hash poseidon for 4 elements
     * @param inputs Poseidon input array of 4 elements
     * @return Poseidon hash
     */
    function _hash4Elements(uint256[4] memory inputs)
    internal
    view
    returns(uint256) {
        return _insPoseidonUnit4.poseidon(inputs);
    }

    /**
     * @dev Hash poseidon for sparse merkle tree final nodes
     * @param key Input element array
     * @param value Input element array
     * @return Poseidon hash1
     */
    function _hashFinalNode(uint256 key, uint256 value)
    public
    view
    returns(uint256) {
        uint256[3] memory inputs;
        inputs[0] = key;
        inputs[1] = value;
        inputs[2] = 1;
        return _hash3Elements(inputs);
    }

    /**
     * @dev Verify sparse merkle tree proof
     * @param root Root to verify
     * @param siblings Siblings necessary to compute the merkle proof
     * @param key Key to verify
     * @param value Value to verify
     * @return True if verification is correct, false otherwise
     */
    function _smtVerifier(
        uint256 root,
        uint256[] calldata siblings,
        uint256 key,
        uint256 value
    ) internal view returns(bool) {
        // Step 2: Calcuate root
        uint256 nextHash = _hashFinalNode(key, value);
        uint256 siblingTmp;
        for (int256 i = int256(siblings.length) - 1; i >= 0; i--) {
            siblingTmp = siblings[uint256(i)];
            bool leftRight = (uint8(key >> uint256(i)) & 0x01) == 1;
            nextHash = leftRight ?
                _hashNode(siblingTmp, nextHash) :
                _hashNode(nextHash, siblingTmp);
        }

        // Step 3: Check root
        return root == nextHash;
    }

    /**
     * @dev Hash poseidon for sparse merkle tree nodes
     * @param left Input element array
     * @param right Input element array
     * @return Poseidon hash
     */
    function _hashNode(uint256 left, uint256 right)
    public
    view
    returns(uint256) {
        uint256[2] memory inputs;
        inputs[0] = left;
        inputs[1] = right;
        return _hash2Elements(inputs);
    }
}