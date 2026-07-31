package platform

type WFPStatus struct {
	Elevated        bool   `json:"elevated"`
	BFERunning      bool   `json:"bfe_running"`
	EngineReady     bool   `json:"engine_ready"`
	RepairAttempted bool   `json:"repair_attempted"`
	Repaired        bool   `json:"repaired"`
	Detail          string `json:"detail,omitempty"`
}
