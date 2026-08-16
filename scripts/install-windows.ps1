<#
.SYNOPSIS
    Installs AgentMux for the current user and creates Desktop and Start Menu
    shortcuts.

.DESCRIPTION
    A per-user install: everything goes under %LOCALAPPDATA%\Programs\AgentMux,
    so it needs no administrator rights and touches nothing outside your profile.

    Your data — servers, projects, agents, keys — lives in %APPDATA%\AgentMux and
    is never touched by this script, so installing over an existing copy keeps
    everything you had.

.PARAMETER Build
    Build from source first. Requires Go and Node. Without it the script expects
    dist\agentmux.exe to exist already.

.PARAMETER Uninstall
    Remove the installed copy and its shortcuts. Your data is left alone.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1 -Build
#>
[CmdletBinding()]
param(
    [switch]$Build,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\AgentMux'
$exePath = Join-Path $installDir 'agentmux.exe'
$iconPath = Join-Path $installDir 'agentmux.ico'
$desktopLnk = Join-Path ([Environment]::GetFolderPath('Desktop')) 'AgentMux.lnk'
$startMenuDir = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'
$startLnk = Join-Path $startMenuDir 'AgentMux.lnk'

# Only the installed copy is stopped. A build you are running out of the repo is
# a different file and is left alone, so installing does not interrupt a session
# you have open.
function Stop-Running {
    $running = @(Get-Process agentmux -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -and $_.Path -ieq $exePath })
    if ($running.Count -eq 0) { return }
    Write-Host "  stopping $($running.Count) running instance(s) of the installed copy"
    $running | Stop-Process -Force
    Start-Sleep -Milliseconds 800
}

if ($Uninstall) {
    Write-Host 'Uninstalling AgentMux' -ForegroundColor Cyan
    Stop-Running
    foreach ($lnk in @($desktopLnk, $startLnk)) {
        if (Test-Path $lnk) { Remove-Item $lnk -Force; Write-Host "  removed $lnk" }
    }
    if (Test-Path $installDir) { Remove-Item $installDir -Recurse -Force; Write-Host "  removed $installDir" }
    Write-Host ''
    Write-Host 'Done. Your data is still in ' -NoNewline
    Write-Host (Join-Path $env:APPDATA 'AgentMux') -ForegroundColor Yellow
    Write-Host 'Delete that folder too if you want a clean slate.'
    return
}

Write-Host 'Installing AgentMux' -ForegroundColor Cyan

if ($Build) {
    Write-Host '  building frontend'
    Push-Location (Join-Path $repoRoot 'frontend')
    if (-not (Test-Path 'node_modules')) { npm install | Out-Null }
    npm run build | Out-Null
    Pop-Location

    Write-Host '  generating icon'
    Push-Location $repoRoot
    go run ./tools/icongen | Out-Null
    Write-Host '  building binary'
    New-Item -ItemType Directory -Force (Join-Path $repoRoot 'dist') | Out-Null
    # -H windowsgui marks the executable as a GUI subsystem binary. Without it
    # Go produces a console binary and Windows opens a black console window
    # behind the app every time it launches.
    go build -ldflags '-H windowsgui' -o (Join-Path $repoRoot 'dist\agentmux.exe') .
    Pop-Location
}

$source = Join-Path $repoRoot 'dist\agentmux.exe'
if (-not (Test-Path $source)) {
    throw "No binary at $source. Re-run with -Build, or build it yourself first."
}
$sourceIcon = Join-Path $repoRoot 'build\appicon\icon.ico'

Stop-Running
New-Item -ItemType Directory -Force $installDir | Out-Null
Copy-Item $source $exePath -Force
if (Test-Path $sourceIcon) { Copy-Item $sourceIcon $iconPath -Force }
Write-Host "  installed to $exePath"

$shell = New-Object -ComObject WScript.Shell
foreach ($target in @($desktopLnk, $startLnk)) {
    $dir = Split-Path -Parent $target
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force $dir | Out-Null }
    $lnk = $shell.CreateShortcut($target)
    $lnk.TargetPath = $exePath
    $lnk.WorkingDirectory = $installDir
    $lnk.Description = 'Multi-server AI agent and SSH cluster control plane'
    # Point at the .ico rather than the exe so the shortcut still shows the mark
    # if the binary is ever rebuilt without its resource object.
    $lnk.IconLocation = if (Test-Path $iconPath) { "$iconPath,0" } else { "$exePath,0" }
    $lnk.Save()
    Write-Host "  shortcut: $target"
}

$dataDir = Join-Path $env:APPDATA 'AgentMux'
Write-Host ''
Write-Host 'Installed.' -ForegroundColor Green
Write-Host "  App    $exePath"
Write-Host "  Data   $dataDir  (untouched by install and uninstall)"
Write-Host ''
Write-Host 'Launch it from the Desktop shortcut or the Start menu.'
Write-Host 'To remove it later:  powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1 -Uninstall'
