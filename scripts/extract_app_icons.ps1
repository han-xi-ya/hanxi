# extract_app_icons.ps1 — N27 一次性图标提取工序（侦查收口见 docs/plans/PLAN_N27_ICON.md）
#
# 用法（开发机手工执行，非 CI 环节）：
#   powershell -File scripts/extract_app_icons.ps1 -Mappings "ccswitch=bin\hanxidata\versions\ccswitch_3.20.1\cc-switch.exe;keyviz=...keyviz.exe"
# 语义：对每个 module=exe 映射，从 PE 资源直接枚举全部 RT_ICON 画幅取**最大真实
# 画幅**（RT_GROUP_ICON 组目录 + 兜底全量 RT_ICON 扫描，PNG 画幅原样取、DIB 画幅
# 交 GDI 解码）→ 等比降采样到 -Px（默认 64）落 frontend/src/assets/apps/<module>.png
# （存在即覆盖）。源小于 Px 时保留源真实尺寸，**绝不上采样凑数**。取不到显式报
# FAIL 退出码非零——宁缺毋滥，绝不为凑图伪造图标。
#
# 通道更替说明（尾账修复）：旧版走 shell32!ExtractIconEx(index=0) 大图标句柄，
# 实测受 DPI/SM_CXICON 牵制——即便 exe 内嵌 256² 画幅也只回 32²（guoheview/keyviz
# 实证），会把高清源静默降采样。改 PE RT_ICON 枚举后按源最大真实画幅取，杜绝复现。
#
# 纪律：产物入库前逐个过 docs/THIRD_PARTY_NOTICES.md 许可条目；Sysinternals
# 系（rammap）等许可受限上游不跑本脚本，落通用徽标 app:generic。
# 入库后必须同步：internal/app/appicons 同名副本（sync_test 逐字节对账）、
# appicons/sources.go 台账边长、menus/ 16px 菜单变体（appicons 包内测试锁死）。
param(
    [Parameter(Mandatory = $true)][string]$Mappings,
    [string]$OutDir = "frontend\src\assets\apps",
    [int]$Px = 64
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$peSrc = @'
using System;
using System.IO;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Collections.Generic;
public static class PeIconExtract {
    delegate bool EnumProc(IntPtr h, IntPtr type, IntPtr name, IntPtr param);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)] static extern IntPtr LoadLibraryExW(string f, IntPtr hFile, uint dwFlags);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)] static extern bool EnumResourceNames(IntPtr h, IntPtr type, EnumProc cb, IntPtr param);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)] static extern IntPtr FindResourceW(IntPtr h, IntPtr name, IntPtr type);
    [DllImport("kernel32.dll")] static extern IntPtr LoadResource(IntPtr h, IntPtr res);
    [DllImport("kernel32.dll")] static extern IntPtr LockResource(IntPtr d);
    [DllImport("kernel32.dll")] static extern uint SizeofResource(IntPtr h, IntPtr res);
    [DllImport("kernel32.dll")] static extern bool FreeLibrary(IntPtr h);
    static IntPtr MI(uint id) { return (IntPtr)id; }

    // 读一个 PE 图标资源集的全部候选画幅（PNG 画幅优先，DIB 走 GDI 解码），
    // 落一张"最大真实画幅"原生 PNG 到 nativePath，返回 "WxH"。
    public static string ExtractNative(string exe, string nativePath) {
        IntPtr h = LoadLibraryExW(exe, IntPtr.Zero, 2 /* LOAD_LIBRARY_AS_DATAFILE */);
        if (h == IntPtr.Zero) return "FAIL:loadlib";
        try {
            int bestW = 0; byte[] best = null; bool bestPng = false;
            List<IntPtr> names = new List<IntPtr>();
            EnumProc cb = delegate(IntPtr a, IntPtr t, IntPtr n, IntPtr p) { names.Add(n); return true; };
            EnumResourceNames(h, MI(3 /* RT_ICON */), cb, IntPtr.Zero);
            GC.KeepAlive(cb);
            foreach (IntPtr n in names) {
                IntPtr r = FindResourceW(h, n, MI(3));
                if (r == IntPtr.Zero) continue;
                uint sz = SizeofResource(h, r);
                if (sz < 12) continue;
                byte[] b = new byte[sz];
                Marshal.Copy(LockResource(LoadResource(h, r)), b, 0, (int)sz);
                bool png = b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47;
                int w;
                if (png) { w = (b[16] << 24) | (b[17] << 16) | (b[18] << 8) | b[19]; }
                else { w = BitConverter.ToInt32(b, 4); }
                if (w > bestW) { bestW = w; best = b; bestPng = png; }
            }
            if (best == null) return "FAIL:no-icon";
            if (bestPng) { File.WriteAllBytes(nativePath, best); }
            else {
                int bh = BitConverter.ToInt32(best, 8) / 2;
                byte[] hdr = new byte[22];
                hdr[2] = 1; hdr[4] = 1;
                hdr[6] = (byte)(bestW == 256 ? 0 : bestW);
                hdr[7] = (byte)(bh == 256 ? 0 : bh);
                hdr[10] = 1;
                Array.Copy(BitConverter.GetBytes((ushort)32), 0, hdr, 12, 2);
                Array.Copy(BitConverter.GetBytes((uint)best.Length), 0, hdr, 14, 4);
                Array.Copy(BitConverter.GetBytes((uint)22), 0, hdr, 18, 4);
                byte[] ico = new byte[22 + best.Length];
                Buffer.BlockCopy(hdr, 0, ico, 0, 22);
                Buffer.BlockCopy(best, 0, ico, 22, best.Length);
                using (MemoryStream ms = new MemoryStream(ico))
                using (Icon ic = new Icon(ms))
                using (Bitmap bmp = ic.ToBitmap())
                    bmp.Save(nativePath, ImageFormat.Png);
            }
            Image img = Image.FromFile(nativePath);
            string dim = img.Width + "x" + img.Height;
            img.Dispose();
            return "OK:" + dim + (bestPng ? ":PNG" : ":DIB");
        } catch (Exception e) { return "FAIL:exc:" + e.Message; }
        finally { FreeLibrary(h); }
    }
}
'@
Add-Type -TypeDefinition $peSrc -ReferencedAssemblies System.Drawing
$root = Split-Path -Parent $PSScriptRoot
$dest = if ([System.IO.Path]::IsPathRooted($OutDir)) { $OutDir } else { Join-Path $root $OutDir }
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("n27extract_" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$fail = 0
try {
    foreach ($pair in $Mappings -split ';') {
        if (-not $pair.Trim()) { continue }
        $module, $exeRel = $pair.Trim() -split '=', 2
        $exe = Join-Path $root $exeRel
        if (-not (Test-Path $exe)) { Write-Output "FAIL $module : exe 不存在 $exe"; $fail++; continue }
        $native = Join-Path $tmp "$module.native.png"
        $r = [PeIconExtract]::ExtractNative($exe, $native)
        if ($r -notlike "OK:*") { Write-Output "FAIL $module : $r（如实降级 app:generic）"; $fail++; continue }
        $src = $r.Substring(3)
        # 等比降采样到 Px：源 ≤ Px 保留真实尺寸直用，源 > Px 高质量缩小，绝不放大
        $img = [System.Drawing.Image]::FromFile($native)
        $sw = $img.Width; $sh = $img.Height
        if ($sw -ne $sh) { $img.Dispose(); Write-Output "FAIL $module : 源非正方 ${sw}x${sh}，人工核验后再入库"; $fail++; continue }
        $out = if ($sw -le $Px) { $sw } else { $Px }
        $canvas = New-Object System.Drawing.Bitmap($out, $out)
        $g = [System.Drawing.Graphics]::FromImage($canvas)
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $g.DrawImage($img, 0, 0, $out, $out)
        $g.Dispose(); $img.Dispose()
        $dst = Join-Path $dest ("$module.png")
        $canvas.Save($dst, [System.Drawing.Imaging.ImageFormat]::Png)
        $canvas.Dispose()
        $tag = if ($out -lt $Px) { "源天花板 $sw，不放大" } else { "源 $sw 缩至 $out" }
        Write-Output "OK $module -> $dst ($src / $tag)"
        if ($out -lt $Px) { Write-Output "  注意：$module 出货 $out 边长与默认 $Px 不同，须同步 appicons/sources.go 台账" }
    }
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
exit $fail
