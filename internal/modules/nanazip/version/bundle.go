package version

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	nanaZipName        = "40174MouriNaruto.NanaZip"
	nanaZipPublisher   = "CN=E310A153-74A9-4D81-800B-857A8D58408A"
	nanaZipAppID       = "NanaZip.Modern"
	maxBundleEntrySize = 128 << 20
	maxBundleTotalSize = 512 << 20
)

type bundleManifest struct {
	Identity struct {
		Name      string `xml:"Name,attr"`
		Publisher string `xml:"Publisher,attr"`
		Version   string `xml:"Version,attr"`
	} `xml:"Identity"`
	Packages []bundlePackage `xml:"Packages>Package"`
}

type bundlePackage struct {
	FileName     string `xml:"FileName,attr"`
	Architecture string `xml:"Architecture,attr"`
	Type         string `xml:"Type,attr"`
	ResourceID   string `xml:"ResourceId,attr"`
}

type appManifest struct {
	Identity struct {
		Name         string `xml:"Name,attr"`
		Publisher    string `xml:"Publisher,attr"`
		Version      string `xml:"Version,attr"`
		Architecture string `xml:"ProcessorArchitecture,attr"`
	} `xml:"Identity"`
	Applications []struct {
		ID string `xml:"Id,attr"`
	} `xml:"Applications>Application"`
}

func inspectBundle(path, expectedVersion string) ([]string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("打开 MSIXBundle: %w", err)
	}
	defer reader.Close()

	var manifestFile *zip.File
	entries := make(map[string]*zip.File, len(reader.File))
	var total uint64
	for _, file := range reader.File {
		clean := filepath.ToSlash(filepath.Clean(file.Name))
		if filepath.IsAbs(file.Name) || clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, fmt.Errorf("Bundle 包含非法路径 %q", file.Name)
		}
		if file.UncompressedSize64 > maxBundleEntrySize {
			return nil, fmt.Errorf("Bundle 条目过大: %s", file.Name)
		}
		total += file.UncompressedSize64
		if total > maxBundleTotalSize {
			return nil, fmt.Errorf("Bundle 解压尺寸超过限制")
		}
		key := strings.ToLower(clean)
		if _, exists := entries[key]; exists {
			return nil, fmt.Errorf("Bundle 包含重复条目 %s", clean)
		}
		entries[key] = file
		if key == "appxmetadata/appxbundlemanifest.xml" {
			manifestFile = file
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("Bundle 缺少 AppxBundleManifest.xml")
	}

	manifestData, err := readZipEntry(manifestFile, 2<<20)
	if err != nil {
		return nil, fmt.Errorf("读取 Bundle manifest: %w", err)
	}
	var manifest bundleManifest
	if err := xml.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("解析 Bundle manifest: %w", err)
	}
	if manifest.Identity.Name != nanaZipName || manifest.Identity.Publisher != nanaZipPublisher || manifest.Identity.Version != expectedVersion {
		return nil, fmt.Errorf("Bundle 身份不匹配: %s %s %s", manifest.Identity.Name, manifest.Identity.Publisher, manifest.Identity.Version)
	}

	architectures := make([]string, 0, len(manifest.Packages))
	seenArch := map[string]bool{}
	compatible := false
	for _, pkg := range manifest.Packages {
		if !strings.EqualFold(pkg.Type, "application") || pkg.ResourceID != "" {
			continue
		}
		arch := strings.ToLower(pkg.Architecture)
		if arch == "" {
			arch = "neutral"
		}
		if !seenArch[arch] {
			architectures = append(architectures, arch)
			seenArch[arch] = true
		}
		if isCompatibleArchitecture(arch) {
			compatible = true
		}
		entry := entries[strings.ToLower(filepath.ToSlash(filepath.Clean(pkg.FileName)))]
		if entry == nil {
			return nil, fmt.Errorf("Bundle 缺少内部包 %s", pkg.FileName)
		}
		if err := inspectInnerPackage(entry, expectedVersion, arch); err != nil {
			return nil, err
		}
	}
	if !compatible {
		return nil, fmt.Errorf("Bundle 不支持当前架构 %s，可用架构: %s", runtime.GOARCH, strings.Join(architectures, ", "))
	}

	// Read every outer entry fully so archive/zip validates CRC32 for the whole bundle.
	for _, file := range reader.File {
		if _, err := readZipEntry(file, maxBundleEntrySize); err != nil {
			return nil, fmt.Errorf("Bundle CRC 校验失败 %s: %w", file.Name, err)
		}
	}
	return architectures, nil
}

func inspectInnerPackage(entry *zip.File, expectedVersion, expectedArch string) error {
	data, err := readZipEntry(entry, maxBundleEntrySize)
	if err != nil {
		return fmt.Errorf("读取内部 MSIX %s: %w", entry.Name, err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("打开内部 MSIX %s: %w", entry.Name, err)
	}
	var manifestFile *zip.File
	for _, file := range reader.File {
		if strings.EqualFold(filepath.ToSlash(filepath.Clean(file.Name)), "AppxManifest.xml") {
			if manifestFile != nil {
				return fmt.Errorf("内部 MSIX %s 包含重复 AppxManifest.xml", entry.Name)
			}
			manifestFile = file
		}
	}
	if manifestFile == nil {
		return fmt.Errorf("内部 MSIX %s 缺少 AppxManifest.xml", entry.Name)
	}
	manifestData, err := readZipEntry(manifestFile, 2<<20)
	if err != nil {
		return err
	}
	var manifest appManifest
	if err := xml.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("解析内部 AppxManifest.xml: %w", err)
	}
	if manifest.Identity.Name != nanaZipName || manifest.Identity.Publisher != nanaZipPublisher || manifest.Identity.Version != expectedVersion {
		return fmt.Errorf("内部 MSIX 身份不匹配: %s %s %s", manifest.Identity.Name, manifest.Identity.Publisher, manifest.Identity.Version)
	}
	if expectedArch != "neutral" && !strings.EqualFold(manifest.Identity.Architecture, expectedArch) {
		return fmt.Errorf("内部 MSIX 架构不匹配: %s != %s", manifest.Identity.Architecture, expectedArch)
	}
	for _, app := range manifest.Applications {
		if app.ID == nanaZipAppID {
			return nil
		}
	}
	return fmt.Errorf("内部 MSIX 缺少应用 %s", nanaZipAppID)
}

func readZipEntry(file *zip.File, limit uint64) ([]byte, error) {
	if file.UncompressedSize64 > limit {
		return nil, fmt.Errorf("entry exceeds limit")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, int64(limit)+1))
}

func isCompatibleArchitecture(arch string) bool {
	if arch == "neutral" {
		return true
	}
	switch runtime.GOARCH {
	case "amd64":
		return arch == "x64" || arch == "amd64"
	case "arm64":
		return arch == "arm64"
	case "386":
		return arch == "x86"
	default:
		return false
	}
}
