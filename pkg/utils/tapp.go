package utils

const (
	TAppIndexLabel = "TAppIndexLabel"
	TAppAPIVersion = "apps.tkestack.io/v1"
	TAppKind       = "TApp"
)

// TAppIndexList represents a list of consecutive index
type TAppIndexList struct {
	// Start of the list
	Start int32 `json:"start"`
	// End of the list
	End int32 `json:"end"`
}
