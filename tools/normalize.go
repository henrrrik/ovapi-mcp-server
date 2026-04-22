package tools

import (
	"math"
	"strings"
)

// Sentinel coordinate upstream returns for stops without a real location —
// it lands somewhere in central France. Roughly 20%+ of stops carry it,
// which breaks any caller that tries to plot or distance-sort.
const (
	sentinelLat  = 47.974766
	sentinelLng  = 3.3135424
	coordEpsilon = 1e-5
)

// cleanCoord returns nil when the input is missing or matches the known
// sentinel. JSON-encoded it serializes as `null`, which is the right signal to
// a client that we don't have a real location.
func cleanCoord(lat, lng float64) *[2]float64 {
	if lat == 0 && lng == 0 {
		return nil
	}
	if math.Abs(lat-sentinelLat) < coordEpsilon && math.Abs(lng-sentinelLng) < coordEpsilon {
		return nil
	}
	return &[2]float64{lat, lng}
}

// townOrEmpty returns a usable town string. Upstream emits the literal string
// "unknown" for most stops; when the stop name follows "<Town>, <Street>" we
// infer the town from the prefix. Otherwise we return "", which callers can
// drop via omitempty.
func townOrEmpty(upstreamTown, stopName string) string {
	t := strings.TrimSpace(upstreamTown)
	if t != "" && !strings.EqualFold(t, "unknown") {
		return t
	}
	if i := strings.Index(stopName, ","); i > 0 {
		candidate := strings.TrimSpace(stopName[:i])
		if candidate != "" {
			return candidate
		}
	}
	return ""
}
