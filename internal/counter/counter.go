// Package counter provides delta computation for monotonically increasing
// network byte counters, shared by the SNMP and SSH polling services.
package counter

import (
	"log/slog"
	"math"
)

// ComputeDelta returns the increase in a 64-bit counter, handling wrap-around
// by assuming a forward-only counter.
func ComputeDelta(previousValue, currentValue uint64) float64 {
	if currentValue >= previousValue {
		return float64(currentValue - previousValue)
	}
	// Counter wrapped. Choose the modulus based on value magnitude: if both
	// values fit in 32 bits it's a 32-bit counter wrap (modulus 2^32),
	// otherwise 64-bit (modulus 2^64).
	if previousValue <= math.MaxUint32 && currentValue <= math.MaxUint32 {
		slog.Warn("32-bit counter wrap-around detected",
			"previous", previousValue, "current", currentValue)
		return float64(math.MaxUint32-previousValue) + float64(currentValue) + 1
	}
	slog.Warn("64-bit counter wrap-around detected",
		"previous", previousValue, "current", currentValue)
	return float64(math.MaxUint64-previousValue) + float64(currentValue) + 1
}
