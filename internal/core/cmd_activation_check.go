package core

// CmdActivationCheck describes a controlled cmd activation test, not the state
// of an arbitrary terminal that was already running when the GUI was started.
type CmdActivationCheck struct {
	Passed bool                `json:"passed"`
	Tools  []CmdToolActivation `json:"tools"`
}

type CmdToolActivation struct {
	Tool       string `json:"tool"`
	Version    string `json:"version"`
	Source     string `json:"source"`
	Before     string `json:"before"`
	Command    string `json:"command"`
	Executable string `json:"executable"`
	Passed     bool   `json:"passed"`
	Error      string `json:"error,omitempty"`
}
