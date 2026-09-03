package dotnetversion

import (
	"os"
	"testing"
)

func TestOfficialSourcesSmoke(t *testing.T) {
	if os.Getenv("HANXI_OFFICIAL_SMOKE") != "1" {
		t.Skip("set HANXI_OFFICIAL_SMOKE=1 to test live .NET release-metadata source")
	}
	channels, err := defaultSource().fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatalf("channels=%#v", channels)
	}
}
