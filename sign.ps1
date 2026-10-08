<#
  sign.ps1 —— 给 exe 加代码签名（自签名证书）

  能做什么：
    · 给程序加数字签名，便于校验文件是否被篡改
    · 对方把导出的 .cer 装进「受信任的根证书颁发机构」后，
      UAC 弹窗会显示发布者名称，而不是「未知发布者」

  不能做什么（重要）：
    · 不能消除 SmartScreen 的「Windows 已保护你的电脑」。
      微软官方文档明确写着：自签名证书在 SmartScreen 上的表现
      与「完全不签名」相同。详见 签名说明.md。

  用法：
    .\sign.ps1                    签名 mes_conv\物料档案转换工具.exe
    .\sign.ps1 -ExePath <路径>    签名指定文件
    .\sign.ps1 -NewCert           强制重新生成证书
    .\sign.ps1 -Timestamp         同时加盖时间戳（需要联网）

  注意：
    · 签名必须是构建的最后一步 —— 签完再改 exe，签名就失效了。
    · 证书私钥留在本机证书存储（Cert:\CurrentUser\My），不会进仓库；
      导出的 .cer 只有公开部分。
    · 让机器「信任」这个证书需要装进受信任根存储。装到 LocalMachine
      会弹系统确认框，所以本脚本不代劳，只打印指引（见输出末尾）。
#>
[CmdletBinding()]
param(
  [string]$ExePath = "",
  [string]$Subject = "CN=Cloak Internal Tool Signing",
  [string]$CerOut = "",
  [switch]$NewCert,
  [switch]$Timestamp
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $ExePath) { $ExePath = Join-Path $root "mes_conv\物料档案转换工具.exe" }
if (-not $CerOut) { $CerOut = Join-Path $root "签名证书.cer" }

if (-not (Test-Path $ExePath)) { throw "找不到要签名的文件：$ExePath" }

# ---- 1. 取证书：没有（或指定 -NewCert）就新建 ----
$cert = $null
if (-not $NewCert) {
  $cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert -ErrorAction SilentlyContinue |
    Where-Object { $_.Subject -eq $Subject } |
    Sort-Object NotAfter -Descending |
    Select-Object -First 1
}

if (-not $cert) {
  Write-Host "生成自签名代码签名证书（有效期 5 年）..."
  $cert = New-SelfSignedCertificate `
    -Type CodeSigningCert `
    -Subject $Subject `
    -KeyUsage DigitalSignature `
    -FriendlyName "MES Tool Internal Signing" `
    -CertStoreLocation "Cert:\CurrentUser\My" `
    -NotAfter (Get-Date).AddYears(5)
  Write-Host "  已创建：$($cert.Thumbprint)"
}

Write-Host "使用证书：$($cert.Subject)"
Write-Host "  指纹    ：$($cert.Thumbprint)"
Write-Host "  有效期至：$($cert.NotAfter)"

# ---- 2. 导出公开证书（给他人安装信任）----
Export-Certificate -Cert $cert -FilePath $CerOut -Force | Out-Null
Write-Host "已导出证书：$CerOut"

# ---- 3. 签名 ----
$sig = $null
if ($Timestamp) {
  try {
    $sig = Set-AuthenticodeSignature -FilePath $ExePath -Certificate $cert `
      -HashAlgorithm SHA256 -TimestampServer "http://timestamp.digicert.com"
  }
  catch {
    Write-Host "  时间戳服务不可用，改为不带时间戳签名"
  }
}
if (-not $sig) {
  $sig = Set-AuthenticodeSignature -FilePath $ExePath -Certificate $cert -HashAlgorithm SHA256
}
Write-Host ""
Write-Host "签名状态：$($sig.Status)"

# ---- 4. 复核签名确实写进去了 ----
$check = Get-AuthenticodeSignature -FilePath $ExePath
if (-not $check.SignerCertificate) {
  throw "签名没有写进去：$($check.StatusMessage)"
}
Write-Host "签名者  ：$($check.SignerCertificate.Subject)"

# ---- 5. 说明验证状态与后续动作 ----
if ($check.Status -ne "Valid") {
  Write-Host ""
  Write-Host "验证状态：$($check.Status)"
  Write-Host "  $($check.StatusMessage)"
  Write-Host "这是自签名证书的正常表现 —— 签名已经写入，只是证书的根还不在本机的受信任列表里。"
}
Write-Host ""
Write-Host "完成的文件：$ExePath"
Write-Host "要一并发给使用者的证书：$CerOut"
Write-Host ""
Write-Host "要让本机 / 对方机器验证通过（UAC 不再显示「未知发布者」），把证书装进受信任根："
Write-Host "  图形界面：双击 $CerOut → 安装证书 → 本地计算机 →"
Write-Host "            把所有证书放入下列存储 → 受信任的根证书颁发机构 → 完成"
Write-Host "  命令行（需要管理员 PowerShell）："
Write-Host "    Import-Certificate -FilePath `"$CerOut`" -CertStoreLocation Cert:\LocalMachine\Root"
Write-Host ""
Write-Host "提醒：自签名不能消除 SmartScreen 的「Windows 已保护你的电脑」，"
Write-Host "      详见同目录的 签名说明.md（里面有真正有效的替代方案）。"
