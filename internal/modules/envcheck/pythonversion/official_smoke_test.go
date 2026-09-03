package pythonversion

import (
	"os"
	"testing"
)

func TestOfficialSourcesSmoke(t *testing.T) {
	if os.Getenv("HANXI_OFFICIAL_SMOKE") != "1" {
		t.Skip("set HANXI_OFFICIAL_SMOKE=1 to test live Python official sources")
	}
	catalog, err := defaultSource().fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Releases) == 0 || len(catalog.Lifecycles) == 0 {
		t.Fatalf("catalog=%#v", catalog)
	}
}
