package registry

// Arc parameters verified against official docs on 2026-10-11. RPC selection
// stays private and explicit; no production endpoint is chosen by this package.
const (
	LocalChainID      int64 = 31337
	ArcTestnetChainID int64 = 5042002
	ArcMinimumFee     int64 = 20_000_000_000 // native USDC base units per gas
)
