// Package jsonstore 提供各托管模块"单个 JSON 配置文件"共享的最小读写公共核。
//
// 此前 25 个模块的 store.go 各自复制同一套样板：os.ReadFile + Unmarshal（缺失/损坏
// 容忍）、MkdirAll + MarshalIndent + tmp(带 pid 后缀) 写 + rename 原子替换 + 失败清 tmp。
// 本包只收口最底层的读写两步（Load/Save）；并发锁、JSON 字段语义、默认值与
// 损坏容忍/严格报错的策略选择，全部留在各模块 store 侧。
//
// 落盘格式与原样板逐字段一致：2 空格缩进 MarshalIndent、tmp 名 "<path>.tmp.<pid>"、
// 目录 0755 / 文件 0644——存量用户配置文件无需迁移。
package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrCorrupt 文件存在但内容不是合法 JSON。损坏容忍型 store 应视同"无内容"
// 按默认值继续；严格型 store（如 frpc/memo）据此拒绝把损坏文件当空库覆盖。
var ErrCorrupt = errors.New("corrupt JSON")

// ErrEmpty 文件存在但为 0 字节（写入半途断电/手工截断的形态）。
// 与 ErrCorrupt 分开定义，便于调用方区分"空文件"与"内容畸形"。
var ErrEmpty = errors.New("empty JSON file")

// Load 读取 path 并将 JSON 反序列化进 v。返回语义与原样板逐分支对齐：
//   - 文件不存在：(false, nil)——尚未初始化，v 保持零值，调用方按默认值继续；
//   - 读取失败（权限等 IO 错误）：(false, err)——原样透传给调用方处置；
//   - 0 字节文件：(false, ErrEmpty 包装)；内容损坏：(false, ErrCorrupt 包装)。
//     注意这两种情况下 v 可能被部分填充（json.Unmarshal 的错误恢复行为），
//     调用方必须仅在 ok==true 时才把 v 的值采信到内存态（与原样板一致）。
//   - 解析成功：(true, nil)。
func Load(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if len(data) == 0 {
		return false, fmt.Errorf("%w: %s", ErrEmpty, path)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("%w: %s: %v", ErrCorrupt, path, err)
	}
	return true, nil
}

// Save 将 v 以 2 空格缩进 JSON 原子写入 path：确保父目录 → 写 "<path>.tmp.<pid>"
// → Sync 刷盘 → rename 替换；任何一步失败都清理临时文件并返回错误，
// rename 失败绝不动目标文件（目标要么旧内容要么新内容，不留半成品）。
func Save(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
