// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {TankRegistry} from "../src/TankRegistry.sol";

interface DeploymentVM {
    function envAddress(string calldata name) external returns (address);
    function startBroadcast(address sender) external;
    function stopBroadcast() external;
}

// The script is compiled/tested locally. Broadcasting requires a future operator
// to explicitly invoke Foundry with --broadcast and their selected wallet.
contract DeployBaseSepolia {
    DeploymentVM private constant vm = DeploymentVM(address(uint160(uint256(keccak256("hevm cheat code")))));

    error WrongChain();
    error MissingSender();

    function run() external returns (TankRegistry registry) {
        if (block.chainid != 84532) revert WrongChain();
        address sender = vm.envAddress("TANK_BASE_SEPOLIA_SENDER");
        if (sender == address(0)) revert MissingSender();
        vm.startBroadcast(sender);
        registry = new TankRegistry();
        vm.stopBroadcast();
    }
}
