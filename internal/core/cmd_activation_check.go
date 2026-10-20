package core

// CmdActivationCheck describes a controlled cmd activation test, not the state
// of an arbitrary terminal that was already running when the GUI was started.
type CmdActivationCheck struct {
	Passed          bool                `json:"passed"`
	Tools           []CmdToolActivation `json:"tools"`
	PathExt         string              `json:"pathext,omitempty"`
	PathExtRepaired bool                `json:"pathext_repaired,omitempty"`
	CmdModes        []string            `json:"cmd_modes,omitempty"`
}

type CmdToolActivation struct {
	Tool            string `json:"tool"`
	Version         string `json:"version"`
	Source          string `json:"source"`
	Before          string `json:"before"`
	Command         string `json:"command"`
	Executable      string `json:"executable"`
	ActualVersion   string `json:"actual_version,omitempty"`
	ExpectedVersion string `json:"expected_version,omitempty"`
	Passed          bool   `json:"passed"`
	Error           string `json:"error,omitempty"`
	Code            string `json:"code,omitempty"`
	Action          string `json:"action,omitempty"`
}
