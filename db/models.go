package db

type Stop struct {
	TPCCode      string  `json:"tpc_code"`
	Name         string  `json:"name"`
	Town         string  `json:"town,omitempty"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	StopAreaCode *string `json:"stop_area_code,omitempty"`
}
