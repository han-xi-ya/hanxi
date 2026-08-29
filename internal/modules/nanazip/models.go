package nanazip

const (
	PackageName   = "40174MouriNaruto.NanaZip"
	PackageFamily = "40174MouriNaruto.NanaZip_gnj4mf6z9tkrc"
	Publisher     = "CN=E310A153-74A9-4D81-800B-857A8D58408A"
	MainAppID     = "NanaZip.Modern"
)

type PackageSnapshot struct {
	Revision         uint64 `json:"revision"`
	ObservedAt       string `json:"observedAt"`
	Installed        bool   `json:"installed"`
	Version          string `json:"version"`
	PackageFullName  string `json:"packageFullName"`
	PackageFamily    string `json:"packageFamily"`
	Architecture     string `json:"architecture"`
	InstallLocation  string `json:"installLocation"`
	PackageStatus    string `json:"packageStatus"`
	OperationID      string `json:"operationId"`
	OperationKind    string `json:"operationKind"`
	OperationState   string `json:"operationState"`
	LastErrorCode    string `json:"lastErrorCode"`
	LastErrorMessage string `json:"lastErrorMessage"`
}

type OperationProgress struct {
	OperationID   string `json:"operationId"`
	Kind          string `json:"kind"`
	TargetVersion string `json:"targetVersion"`
	Stage         string `json:"stage"`
	Done          int64  `json:"done"`
	Total         int64  `json:"total"`
	Message       string `json:"message"`
	Terminal      bool   `json:"terminal"`
	Success       bool   `json:"success"`
	ErrorCode     string `json:"errorCode"`
	ErrorDetail   string `json:"errorDetail"`
}

type OperationAccepted struct {
	OperationID string `json:"operationId"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
}
