package remoteversion

// Relation 表示本机版本与某个官网通道目标版本的关系。
type Relation string

const (
	RelationUnknown         Relation = "unknown"
	RelationNotInstalled    Relation = "not-installed"
	RelationLatest          Relation = "latest"
	RelationUpdateAvailable Relation = "update-available"
	RelationAhead           Relation = "ahead"
)

// Release 表示官网通道中的正式版本。
type Release struct {
	Version   string `json:"version"`
	Published string `json:"published"`
}

// Channel 表示一个官方发布通道。
type Channel struct {
	Key            string    `json:"key"`
	Label          string    `json:"label"`
	Detail         string    `json:"detail"`
	Releases       []Release `json:"releases"`
	Relation       Relation  `json:"relation"`
	RelationDetail string    `json:"relationDetail,omitempty"`
}

func RelationFor(installed bool, local, latest string, compare func(string, string) (int, bool)) Relation {
	if !installed {
		return RelationNotInstalled
	}
	if latest == "" {
		return RelationUnknown
	}
	result, ok := compare(local, latest)
	if !ok {
		return RelationUnknown
	}
	switch {
	case result < 0:
		return RelationUpdateAvailable
	case result > 0:
		return RelationAhead
	default:
		return RelationLatest
	}
}
