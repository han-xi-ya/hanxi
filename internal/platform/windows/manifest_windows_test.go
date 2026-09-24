//go:build windows

package windows

import "testing"

func TestParseExecutionLevel(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want ExecutionLevel
		err  bool
	}{
		{"asInvoker", `<assembly><trustInfo><security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security></trustInfo></assembly>`, ExecutionAsInvoker, false},
		{"highestAvailable", `<assembly xmlns="urn:schemas-microsoft-com:asm.v1"><requestedExecutionLevel level="highestAvailable"/></assembly>`, ExecutionHighestAvailable, false},
		{"requireAdministrator", `<requestedExecutionLevel level="requireAdministrator"/>`, ExecutionRequireAdministrator, false},
		{"unknown-level", `<requestedExecutionLevel level="futureLevel"/>`, ExecutionUnknown, false},
		{"missing", `<assembly><trustInfo/></assembly>`, ExecutionUnknown, false},
		{"malformed", `<assembly><requestedExecutionLevel`, ExecutionUnknown, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseExecutionLevel([]byte(tc.xml))
			if (err != nil) != tc.err {
				t.Fatalf("err=%v, want error=%v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("level=%q, want %q", got, tc.want)
			}
		})
	}
}
