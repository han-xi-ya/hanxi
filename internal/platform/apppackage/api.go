package apppackage

import "context"

// Identity describes the stable identity of a packaged Windows application.
type Identity struct {
	Name      string `json:"name"`
	Family    string `json:"family"`
	Publisher string `json:"publisher"`
	AppID     string `json:"appId"`
}

// Package is the current-user registration returned by Windows.
type Package struct {
	Name              string `json:"name"`
	Family            string `json:"family"`
	Publisher         string `json:"publisher"`
	Version           string `json:"version"`
	PackageFullName   string `json:"packageFullName"`
	Architecture      string `json:"architecture"`
	InstallLocation   string `json:"installLocation"`
	Status            string `json:"status"`
	IsFramework       bool   `json:"isFramework"`
	IsResourcePackage bool   `json:"isResourcePackage"`
}

// InstallOptions limits deployment to a verified local package for the current user.
type InstallOptions struct {
	PackagePath     string   `json:"packagePath"`
	Expected        Identity `json:"expected"`
	ExpectedVersion string   `json:"expectedVersion"`
	AllowDowngrade  bool     `json:"allowDowngrade"`
}

// API manages current-user Windows application packages.
type API interface {
	Query(ctx context.Context, identity Identity) (*Package, error)
	Install(ctx context.Context, options InstallOptions) (*Package, error)
	Uninstall(ctx context.Context, identity Identity, packageFullName string) error
	Activate(ctx context.Context, identity Identity) error
}
