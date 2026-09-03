//go:build windows

package windows

const appPackagePowerShellScript = `
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$InformationPreference = 'SilentlyContinue'
$VerbosePreference = 'SilentlyContinue'
$DebugPreference = 'SilentlyContinue'
$WarningPreference = 'SilentlyContinue'

function Read-Request {
    $inputStream = [Console]::OpenStandardInput()
    $memory = New-Object System.IO.MemoryStream
    $inputStream.CopyTo($memory)
    $json = [Text.Encoding]::UTF8.GetString($memory.ToArray())
    return ($json | ConvertFrom-Json)
}

function Write-Response($response) {
    $json = $response | ConvertTo-Json -Compress -Depth 8
    $bytes = [Text.Encoding]::UTF8.GetBytes($json)
    $output = [Console]::OpenStandardOutput()
    $output.Write($bytes, 0, $bytes.Length)
    $output.Flush()
}

function Convert-Package($package) {
    if ($null -eq $package) { return $null }
    return [ordered]@{
        name = [string]$package.Name
        family = [string]$package.PackageFamilyName
        publisher = [string]$package.Publisher
        version = [string]$package.Version
        packageFullName = [string]$package.PackageFullName
        architecture = [string]$package.Architecture
        installLocation = [string]$package.InstallLocation
        status = [string]$package.Status
        isFramework = [bool]$package.IsFramework
        isResourcePackage = [bool]$package.IsResourcePackage
    }
}

function Find-ExactPackage($identity) {
    $matches = @(Get-AppxPackage -Name ([string]$identity.name) | Where-Object {
        $_.Name -ceq [string]$identity.name -and
        $_.PackageFamilyName -ceq [string]$identity.family -and
        $_.Publisher -ceq [string]$identity.publisher -and
        -not $_.IsFramework -and
        -not $_.IsResourcePackage
    })
    if ($matches.Count -gt 1) {
        throw [System.InvalidOperationException]::new('发现多个匹配的当前用户应用包')
    }
    if ($matches.Count -eq 0) { return $null }
    return $matches[0]
}

function New-ScriptError($record) {
    $exception = $record.Exception
    $hresult = ''
    if ($null -ne $exception) {
        # 注意：.NET HResult 是有符号 int32（如 -2146233079）。[uint32] 直接
        # 转换在 Windows PowerShell 5.1 会溢出抛异常令错误通道自毁；-band
        # 又受十六进制字面量类型解析影响不可靠。Int32.ToString('X8') 按位
        # 格式化为 8 位十六进制（0x80131509），两种版本行为一致。
        $hresult = '0x' + $exception.HResult.ToString('X8')
    }
    $fqid = [string]$record.FullyQualifiedErrorId
    $message = [string]$exception.Message
    $code = 'APP_PACKAGE_DEPLOYMENT_FAILED'
    $friendly = 'Windows 包操作失败'
    $retryable = $false

    switch -Regex ($hresult + ' ' + $fqid) {
        'APP_PACKAGE_NOT_INSTALLED' { $code = 'APP_PACKAGE_NOT_INSTALLED'; $friendly = '应用包尚未安装'; break }
        '80070005|AccessDenied' { $code = 'APP_PACKAGE_ACCESS_DENIED'; $friendly = 'Windows 拒绝了当前用户包操作'; break }
        '80073D02|DeploymentError.*in use' { $code = 'APP_PACKAGE_IN_USE'; $friendly = '目标应用或相关资源正在使用中'; $retryable = $true; break }
        '80073CF3' { $code = 'APP_PACKAGE_DEPENDENCY_MISSING'; $friendly = '应用包依赖或兼容性检查失败'; break }
        '800B0100|800B0101|800B0109|80096010' { $code = 'APP_PACKAGE_SIGNATURE_INVALID'; $friendly = '应用包签名验证失败（证书过期或不可信）'; break }
        '800704C7|OperationStopped' { $code = 'APP_PACKAGE_CANCELLED'; $friendly = '包操作已取消'; $retryable = $true; break }
    }

    return [ordered]@{
        code = $code
        message = $friendly
        detail = $message
        hresult = $hresult
        category = [string]$record.CategoryInfo.Category
        fullyQualifiedErrorId = $fqid
        retryable = $retryable
    }
}

$request = $null
try {
    $request = Read-Request
    if ([int]$request.protocolVersion -ne 1) { throw '不支持的协议版本' }
    if ([string]::IsNullOrWhiteSpace([string]$request.requestId)) { throw '缺少请求标识' }

    $result = $null
    switch ([string]$request.operation) {
        'query' {
            $package = Find-ExactPackage $request.identity
            $result = [ordered]@{ package = Convert-Package $package }
        }
        'install' {
            $path = [string]$request.packagePath
            $extension = [IO.Path]::GetExtension($path).ToLowerInvariant()
            if (-not [IO.Path]::IsPathRooted($path) -or @('.msixbundle', '.appxbundle') -notcontains $extension) {
                throw '安装包路径必须是绝对 MSIXBundle/AppxBundle 文件'
            }
            $parameters = @{
                Path = $path
                DeferRegistrationWhenPackagesAreInUse = $true
                ErrorAction = 'Stop'
            }
            $dependencies = @($request.dependencies)
            if ($dependencies.Count -gt 0 -and -not [string]::IsNullOrWhiteSpace([string]$dependencies[0])) {
                $parameters.DependencyPath = $dependencies
            }
            if ([bool]$request.allowDowngrade) {
                $parameters.ForceUpdateFromAnyVersion = $true
            }
            Add-AppxPackage @parameters
            $package = Find-ExactPackage $request.identity
            if ($null -eq $package) { throw '部署完成后未找到目标应用包' }
            if (-not [string]::IsNullOrWhiteSpace([string]$request.expectedVersion) -and [string]$package.Version -cne [string]$request.expectedVersion) {
                throw ('部署后的版本不匹配：' + [string]$package.Version)
            }
            $result = [ordered]@{ package = Convert-Package $package }
        }
        'uninstall' {
            $package = Find-ExactPackage $request.identity
            if ($null -ne $package) {
                if ([string]$package.PackageFullName -cne [string]$request.packageFullName) {
                    throw '待卸载包与当前注册包不匹配'
                }
                Remove-AppxPackage -Package ([string]$request.packageFullName) -ErrorAction Stop
            }
            $result = [ordered]@{ package = $null }
        }
        'activate' {
            $package = Find-ExactPackage $request.identity
            if ($null -eq $package) {
                $record = [System.Management.Automation.ErrorRecord]::new(
                    [System.InvalidOperationException]::new('应用包尚未安装，无法激活'),
                    'APP_PACKAGE_NOT_INSTALLED',
                    [System.Management.Automation.ErrorCategory]::ObjectNotFound,
                    $null)
                throw $record
            }
            $appId = [string]$request.identity.appId
            if ([string]::IsNullOrWhiteSpace($appId) -or $appId.Contains('!') -or $appId.Contains('\') -or $appId.Contains('/')) {
                throw '应用标识无效'
            }
            $aumid = [string]$request.identity.family + '!' + $appId
            $startInfo = New-Object System.Diagnostics.ProcessStartInfo
            $startInfo.FileName = 'explorer.exe'
            $startInfo.Arguments = 'shell:AppsFolder\' + $aumid
            $startInfo.UseShellExecute = $true
            [System.Diagnostics.Process]::Start($startInfo) | Out-Null
            $result = [ordered]@{ package = Convert-Package $package }
        }
        default { throw '不支持的包管理操作' }
    }

    Write-Response ([ordered]@{
        protocolVersion = 1
        requestId = [string]$request.requestId
        ok = $true
        result = $result
        error = $null
    })
    exit 0
}
catch {
    $requestId = ''
    if ($null -ne $request) { $requestId = [string]$request.requestId }
    Write-Response ([ordered]@{
        protocolVersion = 1
        requestId = $requestId
        ok = $false
        result = $null
        error = New-ScriptError $_
    })
    exit 0
}
`
