# Build the deploy tarball on Windows (PowerShell)
# Copies the FULL project source tree so the Docker build context is complete.
# Usage: powershell -ExecutionPolicy Bypass -File deploy\make-package.ps1

$ErrorActionPreference = "Stop"

$ProjectRoot = $PSScriptRoot
if ($ProjectRoot -and $ProjectRoot -like "*deploy") { $ProjectRoot = Split-Path $ProjectRoot -Parent }

$Date        = Get-Date -Format "yyyyMMdd"
$PackageName = "webb-api-deploy-$Date"
$PackageTar  = Join-Path $ProjectRoot "deploy\$PackageName.tar.gz"
$StageDir    = Join-Path $ProjectRoot "deploy\.stage-$PackageName"
$Dest        = Join-Path $StageDir $PackageName

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Building webb-api deploy package" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "Project root: $ProjectRoot"
Write-Host ""

if (Test-Path $StageDir) { Remove-Item -Recurse -Force $StageDir }
New-Item -ItemType Directory -Path $Dest -Force | Out-Null

# ─────────────────────────────────────────────
# Copy the FULL source tree (robocopy), excluding:
#   - VCS / editor / CI junk
#   - build caches and node_modules / dist
#   - non-required trees (docs, electron, e2e, bin)
#   - runtime data/logs
#   - SECRETS: *.key, .license-selftest (contains a private key)
#   - local artifacts: *.exe *.bak *.lic *.tar.gz .env
# ─────────────────────────────────────────────
$ExcludeDirs = @(
  '.git', '.github', '.agents', '.vscode', '.idea',
  'node_modules', 'dist', '.gocache', '.eslintcache',
  'docs', 'electron', 'e2e', 'bin',
  '.license-selftest',
  'data', 'logs',
  '.stage-*'
)
$ExcludeFiles = @(
  '*.exe', '*.tar.gz', '*.bak', '*.lic', '*.key', '.env'
)

Write-Host "Copying full project tree with robocopy..." -ForegroundColor Cyan
$rcArgs = @($ProjectRoot, $Dest, '/E', '/NFL', '/NDL', '/NP', '/NJH', '/NJS', '/R:1', '/W:1',
            '/XD') + $ExcludeDirs + @('/XF') + $ExcludeFiles
& robocopy @rcArgs | Out-Null
$rc = $LASTEXITCODE
if ($rc -ge 8) {
  throw "robocopy failed with exit code $rc"
}
Write-Host "robocopy done (code $rc)." -ForegroundColor Gray

# Place deploy.sh at the package root (the deploy/ tree copy already contains it too)
$shPath = Join-Path $Dest "deploy.sh"
Copy-Item (Join-Path $ProjectRoot "deploy\deploy.sh") $shPath -Force

# Normalize to LF and write without BOM (Windows edits may leave CRLF)
$content = Get-Content $shPath -Raw -Encoding UTF8
$content = $content -replace "`r`n", "`n"
[System.IO.File]::WriteAllText($shPath, $content, (New-Object System.Text.UTF8Encoding $false))
Write-Host "deploy.sh placed at package root (LF)." -ForegroundColor Gray

# Final safety check: no private keys inside the package
$leaked = Get-ChildItem -Path $Dest -Recurse -Include '*.key','private.key','*.pem' -ErrorAction SilentlyContinue
if ($leaked) {
  Write-Host "WARNING: key-like files found in package:" -ForegroundColor Yellow
  $leaked | ForEach-Object { Write-Host "  $($_.FullName)" -ForegroundColor Yellow }
}

# Compress
Write-Host "Compressing..." -ForegroundColor Cyan
tar czf $PackageTar -C $StageDir $PackageName
Remove-Item -Recurse -Force $StageDir

$Size = [math]::Round((Get-Item $PackageTar).Length / 1MB, 1)
Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Package ready!" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Archive:  $PackageTar"
Write-Host "  Size:     ${Size} MB"
Write-Host ""
Write-Host "  Deployment steps:"
Write-Host "    1. Upload:    scp $PackageName.tar.gz user@server:/tmp/"
Write-Host "    2. Extract:   cd /tmp and tar xzf $PackageName.tar.gz"
Write-Host "    3. Deploy:    cd $PackageName and sudo bash deploy.sh"
Write-Host "    4. Visit:     http://<server-ip>:3000"
Write-Host ""
