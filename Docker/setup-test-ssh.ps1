# Docker/setup-test-ssh.ps1
# Deterministic SSH key setup script for local Docker testing (PowerShell for Windows).
# Generates API client key pair and 3 distinct storage container host keys.
# Pre-populates Docker/api/cdn_known_hosts for strict host-key verification.

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$KeysDir = Join-Path $ScriptDir "keys"
$ApiDir = Join-Path $ScriptDir "api"

Write-Host "=== Initializing Docker Local Test SSH Infrastructure (PowerShell) ===" -ForegroundColor Cyan

if (-not (Test-Path $KeysDir)) {
    New-Item -ItemType Directory -Path $KeysDir -Force | Out-Null
}
if (-not (Test-Path $ApiDir)) {
    New-Item -ItemType Directory -Path $ApiDir -Force | Out-Null
}

# Find ssh-keygen executable
$sshKeygen = "ssh-keygen"
if (-not (Get-Command $sshKeygen -ErrorAction SilentlyContinue)) {
    $fallback = "C:\Windows\System32\OpenSSH\ssh-keygen.exe"
    if (Test-Path $fallback) {
        $sshKeygen = $fallback
    } else {
        Write-Error "ssh-keygen not found on PATH or System32. Please install OpenSSH client."
    }
}

# 1. Generate API Client SSH Key (ed25519)
$ClientKey = Join-Path $KeysDir "storage_test_key"
if (-not (Test-Path $ClientKey)) {
    Write-Host "[*] Generating API Client SSH Key pair ($ClientKey)..." -ForegroundColor Yellow
    & $sshKeygen -t ed25519 -f $ClientKey -N '""' -C "api-client-test" | Out-Null
} else {
    Write-Host "[*] API Client SSH Key already exists: $ClientKey" -ForegroundColor Green
}

# 2. Generate Distinct Host Keys for each Storage Container
$Nodes = @("sg", "us", "lon")
foreach ($Node in $Nodes) {
    $HostKey = Join-Path $KeysDir "ssh_host_${Node}_ed25519_key"
    if (-not (Test-Path $HostKey)) {
        Write-Host "[*] Generating Host Key for storage-$Node ($HostKey)..." -ForegroundColor Yellow
        & $sshKeygen -t ed25519 -f $HostKey -N '""' -C "host-storage-$Node" | Out-Null
    } else {
        Write-Host "[*] Host Key for storage-$Node already exists: $HostKey" -ForegroundColor Green
    }
}

# 3. Pre-populate api/cdn_known_hosts
$KnownHosts = Join-Path $ApiDir "cdn_known_hosts"
Write-Host "[*] Generating $KnownHosts..." -ForegroundColor Yellow

$lines = [System.Collections.Generic.List[string]]::new()
foreach ($Node in $Nodes) {
    $PubFile = Join-Path $KeysDir "ssh_host_${Node}_ed25519_key.pub"
    $pubContent = (Get-Content $PubFile -Raw).Trim()
    $parts = $pubContent -split '\s+'
    if ($parts.Length -ge 2) {
        $keyTypeAndKey = "$($parts[0]) $($parts[1])"
        $lines.Add("storage-$Node $keyTypeAndKey")
        $lines.Add("storage-$Node.cdn-net $keyTypeAndKey")
        $lines.Add("storage-$Node,127.0.0.1 $keyTypeAndKey")
    }
}

# Write with Unix line endings LF
[System.IO.File]::WriteAllText($KnownHosts, ($lines -join "`n") + "`n")

Write-Host "=== SSH Test Setup Complete! ===" -ForegroundColor Green
Write-Host "Private keys stored in: Docker/keys/ (excluded via .gitignore)"
Write-Host "Known hosts written to: Docker/api/cdn_known_hosts (excluded via .gitignore)"
