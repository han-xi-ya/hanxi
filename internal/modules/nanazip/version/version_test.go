package version

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseReleasesBodyStableOnly(t *testing.T) {
	body, _ := json.Marshal([]release{
		{TagName: "6.5.1800.0", PublishedAt: "2026-08-06T00:00:00Z", Assets: []asset{{Name: "NanaZip_6.5.1800.0.msixbundle", URL: "https://example.test/stable", Size: 12, Digest: "sha256:" + repeatHex("a")}}},
		{TagName: "7.0.1800.0", Prerelease: true, Assets: []asset{{Name: "NanaZipPreview_7.0.1800.0.msixbundle", Size: 12, Digest: "sha256:" + repeatHex("b")}}},
		{TagName: "6.4.0", Assets: []asset{{Name: "NanaZip_6.4.0.msixbundle", Size: 12, Digest: "sha256:" + repeatHex("c")}}},
	})
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Version != "6.5.1800.0" {
		t.Fatalf("unexpected releases: %#v", list)
	}
}

func TestInspectBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "NanaZip_6.5.1800.0.msixbundle")
	if err := writeTestBundle(path, "6.5.1800.0", currentTestArchitecture()); err != nil {
		t.Fatal(err)
	}
	architectures, err := inspectBundle(path, "6.5.1800.0")
	if err != nil {
		t.Fatalf("inspectBundle failed: %v", err)
	}
	if len(architectures) != 1 || architectures[0] != currentTestArchitecture() {
		t.Fatalf("unexpected architectures: %#v", architectures)
	}
	if _, err := inspectBundle(path, "6.4.0.0"); err == nil {
		t.Fatal("expected version mismatch")
	}
}

func writeTestBundle(path, version, architecture string) error {
	inner := new(bytes.Buffer)
	innerZip := zip.NewWriter(inner)
	manifest, err := innerZip.Create("AppxManifest.xml")
	if err != nil {
		return err
	}
	_, err = manifest.Write([]byte(`<?xml version="1.0"?><Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"><Identity Name="` + nanaZipName + `" Publisher="` + nanaZipPublisher + `" Version="` + version + `" ProcessorArchitecture="` + architecture + `"/><Applications><Application Id="` + nanaZipAppID + `"/></Applications></Package>`))
	if err != nil {
		return err
	}
	if err := innerZip.Close(); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	outer := zip.NewWriter(file)
	bundleManifest, err := outer.Create("AppxMetadata/AppxBundleManifest.xml")
	if err != nil {
		return err
	}
	_, err = bundleManifest.Write([]byte(`<?xml version="1.0"?><Bundle xmlns="http://schemas.microsoft.com/appx/2013/bundle"><Identity Name="` + nanaZipName + `" Publisher="` + nanaZipPublisher + `" Version="` + version + `"/><Packages><Package Type="application" Architecture="` + architecture + `" FileName="NanaZipPackage_` + version + `_` + architecture + `.msix"/></Packages></Bundle>`))
	if err != nil {
		return err
	}
	packageFile, err := outer.Create("NanaZipPackage_" + version + "_" + architecture + ".msix")
	if err != nil {
		return err
	}
	if _, err := packageFile.Write(inner.Bytes()); err != nil {
		return err
	}
	if err := outer.Close(); err != nil {
		return err
	}
	return file.Close()
}

func currentTestArchitecture() string {
	if isCompatibleArchitecture("x64") {
		return "x64"
	}
	if isCompatibleArchitecture("arm64") {
		return "arm64"
	}
	return "x86"
}

func repeatHex(value string) string {
	var buffer bytes.Buffer
	for range 64 {
		buffer.WriteString(value)
	}
	return buffer.String()
}
