// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.28;

import {TankRegistry} from "../src/TankRegistry.sol";

interface Vm {
    function prank(address sender) external;
    function warp(uint256 timestamp) external;
    function expectRevert(bytes calldata data) external;
}

contract TankRegistryTest {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    TankRegistry private registry;

    address private constant ALICE = address(0xA11CE);
    address private constant BOB = address(0xB0B);

    bytes32 private constant FILE_ID = bytes32(uint256(1));
    bytes32 private constant COMMITMENT = bytes32(uint256(2));

    function setUp() public {
        registry = new TankRegistry();
    }

    function testRegisterAndRetrieve() public {
        vm.warp(1000);
        vm.prank(ALICE);
        registry.tank(FILE_ID, COMMITMENT, 123);

        TankRegistry.Record memory record = registry.getTank(ALICE, FILE_ID);

        require(record.commitment == COMMITMENT, "wrong commitment");
        require(record.fileSize == 123, "wrong size");
        require(record.registeredAt == 1000, "wrong timestamp");
    }

    function testIdenticalRetryPreservesTimestamp() public {
        vm.warp(1000);
        vm.prank(ALICE);
        registry.tank(FILE_ID, COMMITMENT, 123);

        vm.warp(2000);
        vm.prank(ALICE);
        registry.tank(FILE_ID, COMMITMENT, 123);

        TankRegistry.Record memory record = registry.getTank(ALICE, FILE_ID);

        require(record.registeredAt == 1000, "retry changed timestamp");
    }

    function testCannotReplaceCommitment() public {
        vm.prank(ALICE);
        registry.tank(FILE_ID, COMMITMENT, 123);

        vm.expectRevert(abi.encodeWithSelector(TankRegistry.CommitmentConflict.selector));
        vm.prank(ALICE);
        registry.tank(FILE_ID, bytes32(uint256(3)), 123);
    }

    function testCannotReplaceFileSize() public {
        vm.prank(ALICE);
        registry.tank(FILE_ID, COMMITMENT, 123);

        vm.expectRevert(abi.encodeWithSelector(TankRegistry.CommitmentConflict.selector));
        vm.prank(ALICE);
        registry.tank(FILE_ID, COMMITMENT, 456);
    }

    function testRegistrantsHaveSeparateNamespaces() public {
        vm.prank(ALICE);
        registry.tank(FILE_ID, COMMITMENT, 123);

        bytes32 bobCommitment = bytes32(uint256(99));
        vm.prank(BOB);
        registry.tank(FILE_ID, bobCommitment, 456);

        TankRegistry.Record memory aliceRecord = registry.getTank(ALICE, FILE_ID);
        TankRegistry.Record memory bobRecord = registry.getTank(BOB, FILE_ID);

        require(aliceRecord.commitment == COMMITMENT, "Alice record changed");
        require(bobRecord.commitment == bobCommitment, "Bob record missing");
    }

    function testRejectsEmptyValues() public {
        vm.expectRevert(abi.encodeWithSelector(TankRegistry.InvalidFileID.selector));
        registry.tank(bytes32(0), COMMITMENT, 123);

        vm.expectRevert(abi.encodeWithSelector(TankRegistry.InvalidCommitment.selector));
        registry.tank(FILE_ID, bytes32(0), 123);

        vm.expectRevert(abi.encodeWithSelector(TankRegistry.InvalidFileSize.selector));
        registry.tank(FILE_ID, COMMITMENT, 0);
    }

    function testMissingRecordReverts() public {
        vm.expectRevert(abi.encodeWithSelector(TankRegistry.RecordNotFound.selector));
        registry.getTank(ALICE, FILE_ID);
    }

    function testFuzzRegistration(bytes32 fileId, bytes32 commitment, uint64 size) public {
        if (fileId == bytes32(0) || commitment == bytes32(0) || size == 0) {
            return;
        }

        vm.prank(ALICE);
        registry.tank(fileId, commitment, size);

        TankRegistry.Record memory record = registry.getTank(ALICE, fileId);

        require(record.commitment == commitment, "commitment mismatch");
        require(record.fileSize == size, "size mismatch");
    }
}
