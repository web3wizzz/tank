// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.28;

/// @notice Stores immutable file commitments within each registrant's namespace.
/// @dev A registration does not prove ongoing storage or availability.
contract TankRegistry {
    struct Record {
        bytes32 commitment;
        uint64 fileSize;
        uint64 registeredAt;
    }

    mapping(address => mapping(bytes32 => Record)) private records;

    error InvalidFileID();
    error InvalidCommitment();
    error InvalidFileSize();
    error CommitmentConflict();
    error RecordNotFound();

    event Tanked(address indexed registrant, bytes32 indexed fileId, bytes32 commitment, uint64 fileSize);

    /// @notice Register a file under the caller's address.
    /// @dev An identical retry succeeds without changing the original timestamp.
    function tank(bytes32 fileId, bytes32 commitment, uint64 fileSize) external {
        if (fileId == bytes32(0)) revert InvalidFileID();
        if (commitment == bytes32(0)) revert InvalidCommitment();
        if (fileSize == 0) revert InvalidFileSize();

        Record storage existing = records[msg.sender][fileId];

        if (existing.commitment != bytes32(0)) {
            if (existing.commitment != commitment || existing.fileSize != fileSize) {
                revert CommitmentConflict();
            }

            return;
        }

        records[msg.sender][fileId] =
            Record({commitment: commitment, fileSize: fileSize, registeredAt: uint64(block.timestamp)});

        emit Tanked(msg.sender, fileId, commitment, fileSize);
    }

    function getTank(address registrant, bytes32 fileId) external view returns (Record memory) {
        Record memory record = records[registrant][fileId];

        if (record.commitment == bytes32(0)) {
            revert RecordNotFound();
        }

        return record;
    }
}
