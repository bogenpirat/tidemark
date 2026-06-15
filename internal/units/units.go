package units

import "fmt"

const (
	KiB = 1024.0
	MiB = 1024 * KiB
	GiB = 1024 * MiB
	TiB = 1024 * GiB
)

// ScaleUnit describes the divisor and label to use when displaying Y-axis values.
type ScaleUnit struct {
	Divisor float64
	Label   string
}

// GetScaleUnit returns the appropriate ScaleUnit for the given maximum bytes/sec
// value, so that Y-axis labels display in a human-readable unit.
// The minimum unit is KiB/s.
func GetScaleUnit(maximumBytesPerSec float64) ScaleUnit {
	switch {
	case maximumBytesPerSec >= TiB:
		return ScaleUnit{Divisor: TiB, Label: "TiB/s"}
	case maximumBytesPerSec >= GiB:
		return ScaleUnit{Divisor: GiB, Label: "GiB/s"}
	case maximumBytesPerSec >= MiB:
		return ScaleUnit{Divisor: MiB, Label: "MiB/s"}
	default:
		return ScaleUnit{Divisor: KiB, Label: "KiB/s"}
	}
}

// FormatBytesPerSec formats a bytes/sec value with an auto-selected unit,
// e.g. "12.34 MiB/s". The minimum displayed unit is KiB/s.
func FormatBytesPerSec(bytesPerSec float64) string {
	scaleUnit := GetScaleUnit(bytesPerSec)
	scaledValue := bytesPerSec / scaleUnit.Divisor
	return fmt.Sprintf("%.2f %s", scaledValue, scaleUnit.Label)
}

// NiceAxisMax rounds maximumValue up to the next "nice" interval value and returns
// both the rounded maximum and the notch step size, targeting between 4 and 6 notches.
func NiceAxisMax(maximumValue float64) (niceMax float64, stepSize float64) {
	if maximumValue <= 0 {
		return 1.0, 0.25
	}

	// Find a step size that gives 4-6 notches.
	// We compute a raw step then round it to 1, 2, 5, 10, 20, 50...
	rawStep := maximumValue / 5.0
	magnitude := 1.0
	for magnitude*10 < rawStep {
		magnitude *= 10
	}
	for magnitude > rawStep {
		magnitude /= 10
	}

	normalizedStep := rawStep / magnitude
	var roundedStep float64
	switch {
	case normalizedStep <= 1:
		roundedStep = 1
	case normalizedStep <= 2:
		roundedStep = 2
	case normalizedStep <= 5:
		roundedStep = 5
	default:
		roundedStep = 10
	}
	stepSize = roundedStep * magnitude

	// Round the max up to the next multiple of stepSize.
	niceMax = stepSize * (float64(int(maximumValue/stepSize)) + 1)
	return niceMax, stepSize
}
