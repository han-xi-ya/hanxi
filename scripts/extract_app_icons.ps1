# extract_app_icons.ps1 — N27 一次性图标提取工序（侦查收口见 docs/plans/PLAN_N27_ICON.md）
#
# 用法（开发机手工执行，非 CI 环节）：
#   powershell -File scripts/extract_app_icons.ps1 -Mappings "ccswitch=bin\hanxidata\versions\ccswitch_3.20.1\cc-switch.exe;keyviz=...keyviz.exe"
# 语义：对每个 module=exe 映射，取 exe 主图标（index 0，大图标句柄）→ 归一
# 32×32 PNG → frontend/src/assets/apps/<module>.png（存在即覆盖）。取不到
# 显式报 FAIL 退出码非零——宁缺毋滥，绝不为凑图伪造图标。
#
# 纪律：产物入库前逐个过 docs/THIRD_PARTY_NOTICES.md 许可条目；Sysinternals
# 系（rammap）等许可受限上游不跑本脚本，落通用徽标 app:generic。
param(
    [Parameter(Mandatory = $true)][string]$Mappings,
    [string]$OutDir = "frontend\src\assets\apps",
    [int]$Px = 32
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public class ShellIcon {
    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    public static extern uint ExtractIconEx(string file, int index, out IntPtr hLarge, out IntPtr hSmall, uint n);
    [DllImport("user32.dll")]
    public static extern bool DestroyIcon(IntPtr h);
}
'@
$root = Split-Path -Parent $PSScriptRoot
$dest = Join-Path $root $OutDir
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$fail = 0
foreach ($pair in $Mappings -split ';') {
    if (-not $pair.Trim()) { continue }
    $module, $exeRel = $pair.Trim() -split '=', 2
    $exe = Join-Path $root $exeRel
    if (-not (Test-Path $exe)) { Write-Output "FAIL $module : exe 不存在 $exe"; $fail++; continue }
    $big = [IntPtr]::Zero; $small = [IntPtr]::Zero
    $n = [ShellIcon]::ExtractIconEx($exe, 0, [ref]$big, [ref]$small, 1)
    if ($n -lt 1 -or $big -eq [IntPtr]::Zero) {
        Write-Output "FAIL $module : 提取不到主图标（如实降级 app:generic）"; $fail++; continue
    }
    try {
        $ico = [System.Drawing.Icon]::FromHandle($big)
        $canvas = New-Object System.Drawing.Bitmap($Px, $Px)
        $g = [System.Drawing.Graphics]::FromImage($canvas)
        # 高质量缩放（源常为 256²，压到 32² 必须 HighQualityBicubic 防糊边）
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.DrawImage($ico, 0, 0, $Px, $Px)
        $g.Dispose()
        $out = Join-Path $dest ("$module.png")
        $canvas.Save($out, [System.Drawing.Imaging.ImageFormat]::Png)
        $canvas.Dispose()
        Write-Output "OK $module -> $out"
    } finally {
        [void][ShellIcon]::DestroyIcon($big)
        [void][ShellIcon]::DestroyIcon($small)
    }
}
exit $fail
