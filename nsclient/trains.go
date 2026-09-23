package nsclient

import (
	"time"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

// stationTTL is how long the station list is reused before a refresh.
const stationTTL = 24 * time.Hour

// Trains bundles the authenticated client with the station cache the train
// tools need to turn names into station codes.
type Trains struct {
	Client   *Client
	Stations *StationCache
}

// NewTrains wires a client for key over httpc with a station cache.
func NewTrains(key string, httpc ovapiclient.HTTPDoer) *Trains {
	c := New(key, httpc)
	return &Trains{Client: c, Stations: NewStationCache(c, stationTTL)}
}
