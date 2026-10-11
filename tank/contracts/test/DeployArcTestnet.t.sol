// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {TankRegistry} from "../src/TankRegistry.sol";
import {DeployArcTestnet} from "../script/DeployArcTestnet.s.sol";

interface PreparationVM {
    function chainId(uint256 chainId) external;
    function setEnv(string calldata name, string calldata value) external;
    function expectRevert(bytes4 selector) external;
}

contract DeployArcTestnetTest {
    PreparationVM private constant vm = PreparationVM(address(uint160(uint256(keccak256("hevm cheat code")))));

    function testRejectsOtherNetworksBeforeReadingSigner() public {
        vm.chainId(31337);
        DeployArcTestnet deployment = new DeployArcTestnet();
        vm.expectRevert(DeployArcTestnet.WrongChain.selector);
        deployment.run();
    }

    function testRejectsZeroSender() public {
        vm.chainId(5042002);
        vm.setEnv("TANK_ARC_TESTNET_SENDER", "0x0000000000000000000000000000000000000000");
        DeployArcTestnet deployment = new DeployArcTestnet();
        vm.expectRevert(DeployArcTestnet.MissingSender.selector);
        deployment.run();
    }

    function testSimulatesRegistryCreationWithoutNetworkOrWallet() public {
        // Synthetic public address in the isolated Forge VM, never a pilot input.
        vm.chainId(5042002);
        vm.setEnv("TANK_ARC_TESTNET_SENDER", "0x1111111111111111111111111111111111111111");
        TankRegistry registry = new DeployArcTestnet().run();
        require(address(registry).code.length > 0, "registry simulation did not create code");
    }
}
