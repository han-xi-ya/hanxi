package nanazip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/nanazip/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/settings"
)

var packageIdentity = apppackage.Identity{Name: PackageName, Family: PackageFamily, Publisher: Publisher, AppID: MainAppID}

type versionManager interface {
	ListReleases() ([]version.Release, error)
	ListCached() ([]version.CachedPackage, error)
	EnsureCached(string, func(version.DownloadProgress)) (version.CachedPackage, error)
	RemoveCached(string) error
}

type NanaZipService struct {
	plat     platform.Platform
	packages apppackage.API
	manager  versionManager

	operationMu sync.Mutex
	operation   *operationState
	revision    atomic.Uint64
}

type operationState struct {
	id      string
	kind    string
	version string
}

func NewNanaZipService(plat platform.Platform) *NanaZipService {
	return newNanaZipService(plat, plat.AppPackage(), version.NewManager(settings.GetPaths().VersionsDir()))
}

func newNanaZipService(plat platform.Platform, packages apppackage.API, manager versionManager) *NanaZipService {
	return &NanaZipService{plat: plat, packages: packages, manager: manager}
}

func (s *NanaZipService) GetPackageSnapshot() (PackageSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return s.querySnapshot(ctx)
}

func (s *NanaZipService) ListReleases() ([]version.Release, error) { return s.manager.ListReleases() }
func (s *NanaZipService) ListCachedPackages() ([]version.CachedPackage, error) {
	return s.manager.ListCached()
}
func (s *NanaZipService) RepoURL() string { return version.RepoURL() }
func (s *NanaZipService) OpenRepo() error { return s.plat.OpenURL(version.RepoURL()) }

func (s *NanaZipService) InstallVersion(targetVersion string, allowDowngrade bool) (OperationAccepted, error) {
	targetVersion = strings.TrimSpace(targetVersion)
	before, err := s.GetPackageSnapshot()
	if err != nil {
		return OperationAccepted{}, err
	}
	if before.Installed {
		comparison := versioncmp.Compare(targetVersion, before.Version)
		if comparison == 0 {
			return OperationAccepted{Kind: "already-installed", Message: "该版本已安装"}, nil
		}
		if comparison < 0 && !allowDowngrade {
			return OperationAccepted{}, fmt.Errorf("需要确认降级：当前 %s，目标 %s", before.Version, targetVersion)
		}
	}
	kind := "install"
	if before.Installed {
		if versioncmp.Compare(targetVersion, before.Version) > 0 {
			kind = "update"
		} else {
			kind = "downgrade"
		}
	}
	op, err := s.beginOperation(kind, targetVersion)
	if err != nil {
		return OperationAccepted{}, err
	}
	go s.runInstall(op, allowDowngrade)
	return OperationAccepted{OperationID: op.id, Kind: kind, Message: "NanaZip 包操作已开始"}, nil
}

func (s *NanaZipService) Uninstall() (OperationAccepted, error) {
	before, err := s.GetPackageSnapshot()
	if err != nil {
		return OperationAccepted{}, err
	}
	if !before.Installed {
		return OperationAccepted{Kind: "already-uninstalled", Message: "NanaZip 尚未安装"}, nil
	}
	op, err := s.beginOperation("uninstall", before.Version)
	if err != nil {
		return OperationAccepted{}, err
	}
	go s.runUninstall(op, before.PackageFullName)
	return OperationAccepted{OperationID: op.id, Kind: "uninstall", Message: "NanaZip 卸载已开始"}, nil
}

func (s *NanaZipService) Launch() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pkg, err := s.packages.Query(ctx, packageIdentity)
	if err != nil {
		return err
	}
	if pkg == nil {
		return fmt.Errorf("NanaZip 尚未安装")
	}
	return s.packages.Activate(ctx, packageIdentity)
}

func (s *NanaZipService) RemoveCachedPackage(targetVersion string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.operation != nil {
		return fmt.Errorf("NanaZip 正在执行 %s，请稍后再删除缓存", s.operation.kind)
	}
	return s.manager.RemoveCached(strings.TrimSpace(targetVersion))
}

func (s *NanaZipService) beginOperation(kind, targetVersion string) (*operationState, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.operation != nil {
		return nil, fmt.Errorf("NanaZip 正在执行 %s", s.operation.kind)
	}
	op := &operationState{id: fmt.Sprintf("nanazip-%d", time.Now().UnixNano()), kind: kind, version: targetVersion}
	s.operation = op
	s.emitProgress(OperationProgress{OperationID: op.id, Kind: kind, TargetVersion: targetVersion, Stage: "preflight", Message: "正在检查系统包状态"})
	return op, nil
}

func (s *NanaZipService) finishOperation(op *operationState) {
	s.operationMu.Lock()
	if s.operation == op {
		s.operation = nil
	}
	s.operationMu.Unlock()
}

func (s *NanaZipService) runInstall(op *operationState, allowDowngrade bool) {
	defer func() {
		s.finishOperation(op)
		s.emitSnapshot()
	}()
	emitVersion := func(progress version.DownloadProgress) {
		stage := progress.Stage
		message := progress.Message
		if stage == "done" {
			stage = "cache-commit"
			if message == "" {
				message = "可信安装包缓存已就绪"
			}
		}
		s.emitProgress(OperationProgress{OperationID: op.id, Kind: op.kind, TargetVersion: op.version, Stage: stage, Done: progress.Done, Total: progress.Total, Message: message})
	}
	cached, err := s.manager.EnsureCached(op.version, emitVersion)
	if err != nil {
		s.failOperation(op, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	before, err := s.packages.Query(ctx, packageIdentity)
	if err != nil {
		s.failOperation(op, err)
		return
	}
	if before != nil {
		comparison := versioncmp.Compare(op.version, before.Version)
		if comparison == 0 {
			s.succeedOperation(op, "目标版本已由外部操作安装")
			return
		}
		if comparison < 0 && !allowDowngrade {
			s.failOperation(op, fmt.Errorf("系统版本已变化，当前 %s 高于目标 %s，需要重新确认降级", before.Version, op.version))
			return
		}
	}

	s.emitProgress(OperationProgress{OperationID: op.id, Kind: op.kind, TargetVersion: op.version, Stage: "installing", Message: "正在交由 Windows 部署 NanaZip"})
	_, err = s.packages.Install(ctx, apppackage.InstallOptions{PackagePath: cached.Path, Expected: packageIdentity, ExpectedVersion: op.version, AllowDowngrade: allowDowngrade})
	if err != nil {
		s.failOperation(op, err)
		return
	}
	after, err := s.packages.Query(ctx, packageIdentity)
	if err != nil {
		s.failOperation(op, err)
		return
	}
	if after == nil || after.Version != op.version {
		s.failOperation(op, fmt.Errorf("部署完成后系统版本未变为 %s", op.version))
		return
	}
	s.succeedOperation(op, fmt.Sprintf("NanaZip %s 已安装", op.version))
}

func (s *NanaZipService) runUninstall(op *operationState, packageFullName string) {
	defer func() {
		s.finishOperation(op)
		s.emitSnapshot()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	s.emitProgress(OperationProgress{OperationID: op.id, Kind: op.kind, TargetVersion: op.version, Stage: "uninstalling", Message: "正在卸载当前用户 NanaZip"})
	if err := s.packages.Uninstall(ctx, packageIdentity, packageFullName); err != nil {
		s.failOperation(op, err)
		return
	}
	after, err := s.packages.Query(ctx, packageIdentity)
	if err != nil {
		s.failOperation(op, err)
		return
	}
	if after != nil {
		s.failOperation(op, fmt.Errorf("Windows 回查仍显示 NanaZip 已安装"))
		return
	}
	s.succeedOperation(op, "NanaZip 已从当前用户卸载；安装包缓存仍保留")
}

func (s *NanaZipService) querySnapshot(ctx context.Context) (PackageSnapshot, error) {
	pkg, err := s.packages.Query(ctx, packageIdentity)
	if err != nil {
		return PackageSnapshot{}, err
	}
	snapshot := PackageSnapshot{Revision: s.revision.Add(1), ObservedAt: time.Now().Format(time.RFC3339), PackageFamily: PackageFamily}
	if pkg != nil {
		snapshot.Installed = true
		snapshot.Version = pkg.Version
		snapshot.PackageFullName = pkg.PackageFullName
		snapshot.Architecture = pkg.Architecture
		snapshot.InstallLocation = pkg.InstallLocation
		snapshot.PackageStatus = pkg.Status
	}
	s.operationMu.Lock()
	if s.operation != nil {
		snapshot.OperationID, snapshot.OperationKind, snapshot.OperationState = s.operation.id, s.operation.kind, "running"
	}
	s.operationMu.Unlock()
	return snapshot, nil
}

func (s *NanaZipService) emitProgress(progress OperationProgress) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("nanazip:operation-progress", progress)
	}
}

func (s *NanaZipService) emitSnapshot() {
	snapshot, err := s.GetPackageSnapshot()
	if err == nil {
		if app := application.Get(); app != nil && app.Event != nil {
			app.Event.Emit("nanazip:package-snapshot", snapshot)
		}
	}
}

func (s *NanaZipService) failOperation(op *operationState, err error) {
	code := "NANAZIP_OPERATION_FAILED"
	var packageErr *apppackage.Error
	if errors.As(err, &packageErr) {
		code = packageErr.Code
	}
	s.emitProgress(OperationProgress{OperationID: op.id, Kind: op.kind, TargetVersion: op.version, Stage: "error", Message: err.Error(), Terminal: true, ErrorCode: code, ErrorDetail: err.Error()})
	notify.Error(ID, "NanaZip 操作失败", err.Error(), "/ext/nanazip")
}

func (s *NanaZipService) succeedOperation(op *operationState, message string) {
	s.emitProgress(OperationProgress{OperationID: op.id, Kind: op.kind, TargetVersion: op.version, Stage: "done", Message: message, Terminal: true, Success: true})
	notify.Success(ID, "NanaZip 操作完成", message, "/ext/nanazip")
}
