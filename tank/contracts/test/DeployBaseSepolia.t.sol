// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {TankRegistry} from "../src/TankRegistry.sol";
import {DeployBaseSepolia} from "../script/DeployBaseSepolia.s.sol";

interface PreparationVM {
    function chainId(uint256 chainId) external;
    function setEnv(string calldata name, string calldata value) external;
    function expectRevert(bytes4 selector) external;
}

contract DeployBaseSepoliaTest {
    PreparationVM private constant vm = PreparationVM(address(uint160(uint256(keccak256("hevm cheat code")))));

    function testRejectsOtherNetworksBeforeReadingSigner() public {
        vm.chainId(31337);
        DeployBaseSepolia deployment = new DeployBaseSepolia();
        vm.expectRevert(DeployBaseSepolia.WrongChain.selector);
        deployment.run();
    }

    function testRejectsZeroSender() public {
        vm.chainId(84532);
        vm.setEnv("TANK_BASE_SEPOLIA_SENDER", "0x0000000000000000000000000000000000000000");
        DeployBaseSepolia deployment = new DeployBaseSepolia();
        vm.expectRevert(DeployBaseSepolia.MissingSender.selector);
        deployment.run();
    }

    function testSimulatesRegistryCreationWithoutNetworkOrWallet() public {
        // Synthetic public address in the isolated Forge VM, never a pilot input.
        vm.chainId(84532);
        vm.setEnv("TANK_BASE_SEPOLIA_SENDER", "0x1111111111111111111111111111111111111111");
        TankRegistry registry = new DeployBaseSepolia().run();
        require(address(registry).code.length > 0, "registry simulation did not create code");
    }
}
