package snapshot

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDecideTick(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	written := base.Add(-10 * time.Minute) // 空闲已超 300s
	okDefault := tickInput{
		Enabled:     true,
		Found:       true,
		MaxMT:       written,
		ScannedMT:   written.Add(-time.Hour),
		Idle:        5 * time.Minute,
		Interval:    5 * time.Minute,
		Deactivated: true,
	}
	cases := []struct {
		name string
		mut  func(*tickInput, *time.Time)
		want tickDecision
	}{
		{"默认-失活+脏+空闲→拍", func(*tickInput, *time.Time) {}, tickGo},
		{"开关关", func(i *tickInput, _ *time.Time) { i.Enabled = false }, skipDisabled},
		{"在途", func(i *tickInput, _ *time.Time) { i.InFlight = true }, skipBusy},
		{"手动直通（免脏判/免空闲）", func(i *tickInput, _ *time.Time) {
			i.Force, i.Found, i.Deactivated = true, false, false
		}, tickGo},
		{"无文件", func(i *tickInput, _ *time.Time) { i.Found = false }, skipQuiet},
		{"mtime 未推进", func(i *tickInput, _ *time.Time) { i.ScannedMT = i.MaxMT }, skipQuiet},
		{"未空闲且未失活", func(i *tickInput, n *time.Time) {
			i.Deactivated = false
			i.MaxMT = n.Add(-time.Minute)
		}, skipActive},
		{"间隔闸", func(i *tickInput, _ *time.Time) { i.LastCommitAt = written.Add(9 * time.Minute) }, skipInterval},
		{"间隔闸已过", func(i *tickInput, _ *time.Time) { i.LastCommitAt = written.Add(-time.Hour) }, tickGo},
		{"从未提交", func(i *tickInput, _ *time.Time) { i.LastCommitAt = time.Time{} }, tickGo},
		{"空闲命中（未失活但静默够久）", func(i *tickInput, n *time.Time) {
			i.Deactivated = false
			i.MaxMT = n.Add(-6 * time.Minute)
		}, tickGo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := okDefault
			now := base
			c.mut(&in, &now)
			if got := decideTick(in, now); got != c.want {
				t.Errorf("decideTick = %v, want %v", got, c.want)
			}
		})
	}
}

func TestParsePorcelain(t *testing.T) {
	// fixture 逐字节模拟 `git status --porcelain=v1 -z` 输出（记录以 NUL 收尾，
	// R/C 记录后多一段原路径 NUL）
	raw := strings.Join([]string{
		" M config.json",
		"?? state/new.json",
		"A  state/added.json",
		" D state/gone.json",
		"R  memo/new.md",
		"memo/old.md",
	}, "\x00") + "\x00"
	entries := parsePorcelain(raw)
	if len(entries) != 5 {
		t.Fatalf("want 5 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Status != " M" || entries[0].Path != "config.json" {
		t.Errorf("entry0 = %+v", entries[0])
	}
	if entries[4].Path != "memo/new.md" || entries[4].Orig != "memo/old.md" {
		t.Errorf("rename entry = %+v", entries[4])
	}
	if entries[3].Path != "state/gone.json" {
		t.Errorf("delete entry = %+v", entries[3])
	}
}

func TestParseGitLog(t *testing.T) {
	// fixture：git log -z --pretty=format:%H%x1f%ct%x1f%s 双记录
	h1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	h2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	unix1 := time.Date(2026, 9, 17, 10, 0, 0, 0, time.Local).Unix()
	unix2 := time.Date(2026, 9, 16, 8, 30, 0, 0, time.Local).Unix()
	raw := h1 + "\x1f" + strconv.FormatInt(unix1, 10) + "\x1fcheckpoint: config.json, memo/a.md\x00" +
		h2 + "\x1f" + strconv.FormatInt(unix2, 10) + "\x1fcheckpoint: state/projects.json\x00"
	revs := parseGitLog(raw)
	if len(revs) != 2 {
		t.Fatalf("want 2 revisions, got %d: %+v", len(revs), revs)
	}
	if revs[0].ID != h1 || revs[0].Summary != "config.json, memo/a.md" {
		t.Errorf("rev0 = %+v", revs[0])
	}
	if got, err := time.Parse(time.RFC3339, revs[0].Time); err != nil || !got.Equal(time.Unix(unix1, 0)) {
		t.Errorf("rev0 time = %q", revs[0].Time)
	}
	if revs[1].Summary != "state/projects.json" {
		t.Errorf("rev1 = %+v", revs[1])
	}
}

func TestParseNameStatus(t *testing.T) {
	out := "M\tconfig.json\nA\tstate/memo.json\nD\tstate/gone.json\nR100\tmemo/old.md\tmemo/new.md\n"
	files := parseNameStatus(out)
	if len(files) != 4 {
		t.Fatalf("want 4, got %+v", files)
	}
	if files[0] != (RevisionFile{Path: "config.json", Status: "M"}) {
		t.Errorf("files0 = %+v", files[0])
	}
	if files[3] != (RevisionFile{Path: "memo/new.md", Status: "R"}) {
		t.Errorf("rename row = %+v", files[3])
	}
}

func TestSummarizeFiles(t *testing.T) {
	if got := summarizeFiles([]string{"config.json"}); got != "config.json" {
		t.Errorf("summarize = %q", got)
	}
	five := []string{"a.json", "state/b.json", "state/c.json", "memo/d.md", "e.json"}
	if got := summarizeFiles(five); got != "a.json, b.json, c.json, d.md, e.json" {
		t.Errorf("summarize5 = %q", got)
	}
	six := append(five, "state/f.json")
	if got := summarizeFiles(six); got != "a.json, b.json, c.json, d.md, e.json 等 6 个文件" {
		t.Errorf("summarize6 = %q", got)
	}
}

func TestCheckRevisionID(t *testing.T) {
	for _, ok := range []string{"abc1234", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "20260917-143000", "20260917-143000-2"} {
		if err := checkRevisionID(ok); err != nil {
			t.Errorf("checkRevisionID(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "zz1234", "--help", "2026-09-17", "../x"} {
		if err := checkRevisionID(bad); err == nil {
			t.Errorf("checkRevisionID(%q) should fail", bad)
		}
	}
}

func TestNormalizeWhitelistPath(t *testing.T) {
	if got, err := normalizeWhitelistPath("config.json"); err != nil || got != "config.json" {
		t.Errorf("config.json: %v %v", got, err)
	}
	if got, err := normalizeWhitelistPath(`state\memo.json`); err != nil || got != "state/memo.json" {
		t.Errorf("backslash: %v %v", got, err)
	}
	for _, bad := range []string{"", "/config.json", "../config.json", "state/../config.json", "runtime/x.toml", "logs/x.log"} {
		if _, err := normalizeWhitelistPath(bad); err == nil {
			t.Errorf("normalizeWhitelistPath(%q) should fail", bad)
		}
	}
}
