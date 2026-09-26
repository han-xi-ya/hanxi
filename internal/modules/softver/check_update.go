// check_update.go 实现 extapi.UpdateChecker 可选契约：把"本机微信有官方新版"
// 接入宿主级"可用更新"雷达（internal/updatewatch 调度器 → registry.SetHealth
// → 首页/模块中心 update-available 徽标，前端零新 RPC）。
// 根因背景：雷达感知面 = 装配根 collectUpdateCheckers 对注册模块的类型断言
// 收集，softver 此前未实现该契约——softver 页内探测一直正常（真机 4.1.15.9
// 三读数命中），微信却永远缺席全局雷达。本文件补的就是这根线。
//
// 廉价纪律（契约注释口径）：
//   - 本地 = 与页内 Snapshot 同源的 probeLocal（注册表 Uninstall 三根 × PE 双
//     口径，纯本地 IO，毫秒级），两代微信共存时取可比的最高读数；
//   - 远程 = 官方更新页读数（2026-09-17 实连验证的 SSR 解析链），带 10 分钟
//     新鲜窗口——与 RefreshOfficial 共享同一缓存，不建第二份真相；
//   - 本机未装微信：返回 false 且不发起外呼（无更新可言，不谎报）；
//   - 远程抓取/解析失败：错误上抛，调度器保持本模块原健康值（失败≠无更新）。

package softver

import (
	"context"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/platform/versioncmp"
)

// officialRadarTTL 与 updatewatch 托管模块 remoteCache 的 10 分钟 TTL 同规：
// 雷达重复感知不放大为官方页重复抓取。
const officialRadarTTL = 10 * time.Minute

// CheckUpdate 实现 extapi.UpdateChecker：宿主调度器直调模块面（非 Wails
// 绑定方法，不经前端调用门），判定语义见 SoftverService.checkUpdate。
func (m *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return m.svc.checkUpdate(ctx)
}

// 编译期断言：雷达契约签名漂移即编译失败。
var _ extapi.UpdateChecker = (*Module)(nil)

// checkUpdate 本机×官方双口径对照。local 为空串（未安装，或版本读数非规范
// 数字段无可比事实）时按契约返回 false 且不发起网络外呼；remote 只在判定
// 成功时有值（调度器 update-available 时投影展示）。
func (s *SoftverService) checkUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	data, perr := s.probeLocal()
	if perr != nil {
		return "", "", false, perr
	}
	local = bestComparableVersion(data.Installs)
	if local == "" {
		return "", "", false, nil
	}
	off, oerr := s.officialForRadar(ctx)
	if oerr != nil {
		return local, "", false, oerr
	}
	return local, off.Version, versioncmp.Compare(off.Version, local) > 0, nil
}

// bestComparableVersion 全部本机安装（3.x/4.x 双代可共存）的主口径读数里，
// 取通过数字段门槛（validVersionRe，与 enrichInstall 主版本判定同口径）的最高
// 版本；一个都不过门槛返回空串 = 无可比事实。
func bestComparableVersion(installs []LocalInstall) string {
	best := ""
	for _, in := range installs {
		v := in.BestVersion
		if v == "" || !validVersionRe.MatchString(v) {
			continue
		}
		if best == "" || versioncmp.Compare(v, best) > 0 {
			best = v
		}
	}
	return best
}

// officialForRadar 新鲜窗口内直接复用页内官方缓存；否则抓取并按与
// RefreshOfficial 相同的口径回写缓存（成功进 official、失败进 officialErr，
// 页内降级横幅同源可见）。抓取在锁外进行（网络不得压页内读锁）；与手动刷新
// 并发时两路同源自净，后写者胜。
func (s *SoftverService) officialForRadar(ctx context.Context) (*OfficialRelease, error) {
	if off := s.freshOfficial(); off != nil {
		return off, nil
	}
	rel, err := s.fetchOfficial(ctx)
	s.mu.Lock()
	if err != nil {
		s.officialErr = err.Error()
	} else {
		s.official = rel
		s.officialErr = ""
	}
	s.mu.Unlock()
	return rel, err
}

// freshOfficial 官方读数仍在新鲜窗口内时返回值拷贝（防调用方拿到缓存指针
// 后被并发覆盖），未缓存/过期/时间戳不可解析都按"需重取"返回 nil。
func (s *SoftverService) freshOfficial() *OfficialRelease {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.official == nil || s.official.Version == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s.official.FetchedAt)
	if err != nil || time.Since(t) >= officialRadarTTL {
		return nil
	}
	off := *s.official
	return &off
}
