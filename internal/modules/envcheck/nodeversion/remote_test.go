package nodeversion

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLTSValue(t *testing.T) {
	for _, raw := range []string{"false", `"Krypton"`} {
		var value ltsValue
		if err := value.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		if raw == "false" && value.Name != "" {
			t.Fatalf("false name=%q", value.Name)
		}
		if raw != "false" && value.Name != "Krypton" {
			t.Fatalf("string name=%q", value.Name)
		}
	}
}

func TestNormalizeLTSAndCurrent(t *testing.T) {
	releases := []nodeRelease{
		{Version: "v24.20.0", Date: "2026-08-26", LTS: ltsValue{Name: "Krypton"}},
		{Version: "v26.8.1", Date: "2026-08-26"},
		{Version: "v24.19.0", Date: "2026-07-01", LTS: ltsValue{Name: "Krypton"}},
		{Version: "v27.0.0-rc.1", Date: "2026-08-30"},
		{Version: "v22.22.0", Date: "2026-07-01", LTS: ltsValue{Name: "Jod"}},
	}
	schedule := map[string]scheduleEntry{
		"v22": {Start: "2024-04-24", LTS: "2024-10-29", Maintenance: "2025-10-21", End: "2027-04-30"},
		"v24": {Start: "2025-05-06", LTS: "2025-10-28", Maintenance: "2026-10-20", End: "2028-04-30", Codename: "Krypton"},
		"v26": {Start: "2026-05-05", LTS: "2026-10-28", Maintenance: "2027-10-20", End: "2029-04-30"},
	}
	got := normalize(releases, schedule, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if len(got) != 2 {
		t.Fatalf("channels=%#v", got)
	}
	if got[0].Key != "lts" || got[0].Releases[0].Version != "24.20.0" || got[0].Detail != "Krypton · Active LTS" {
		t.Fatalf("lts=%#v", got[0])
	}
	if got[1].Key != "current" || got[1].Releases[0].Version != "26.8.1" {
		t.Fatalf("current=%#v", got[1])
	}
}

func TestNormalizeExcludesEOL(t *testing.T) {
	releases := []nodeRelease{{Version: "v25.5.0", Date: "2026-05-01"}}
	schedule := map[string]scheduleEntry{"v25": {Start: "2025-10-15", End: "2026-06-01"}}
	got := normalize(releases, schedule, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if len(got) != 0 {
		t.Fatalf("channels=%#v", got)
	}
}

func TestSourceFetch(t *testing.T) {
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"version":"v26.8.1","date":"2026-08-26","lts":false},
			{"version":"v24.20.0","date":"2026-08-26","lts":"Krypton"}
		]`))
	}))
	defer index.Close()
	schedule := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"v24":{"start":"2025-05-06","lts":"2025-10-28","maintenance":"2026-10-20","end":"2028-04-30"},
			"v26":{"start":"2026-05-05","lts":"2026-10-28","maintenance":"2027-10-20","end":"2029-04-30"}
		}`))
	}))
	defer schedule.Close()
	s := source{indexClient: index.Client(), scheduleClient: schedule.Client(), indexEndpoint: index.URL, scheduleEndpoint: schedule.URL, now: func() time.Time {
		return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	}}
	channels, err := s.fetch()
	if err != nil || len(channels) != 2 {
		t.Fatalf("channels=%#v err=%v", channels, err)
	}
}
