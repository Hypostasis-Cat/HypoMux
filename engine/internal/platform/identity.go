package platform

// Identity describes security-relevant properties of the engine host process.
// It intentionally contains only values that every UI implementation may need.
type Identity struct {
	ProcessID int  `json:"pid"`
	Elevated  bool `json:"elevated"`
}
