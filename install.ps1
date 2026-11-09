# Installs Codist on Windows, then checks what it needs. In PowerShell:
#
#   irm https://raw.githubusercontent.com/abxengg/codist/remaster/install.ps1 | iex
#
# Settings, all optional, as environment variables:
#   CODIST_VERSION   a release tag such as v0.1.0 (default: the latest release)
#   CODIST_BIN_DIR   where to put codist.exe (default: %LOCALAPPDATA%\codist\bin)
#   CODIST_FROM_SOURCE=1  build from source with Go instead of downloading
$ErrorActionPreference = 'Stop'

$Repo    = 'abxengg/codist'
$Branch  = 'remaster'
$BinDir  = if ($env:CODIST_BIN_DIR) { $env:CODIST_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'codist\bin' }
$Version = if ($env:CODIST_VERSION) { $env:CODIST_VERSION } else { 'latest' }
$Exe     = Join-Path $BinDir 'codist.exe'

function Have($name) { [bool](Get-Command $name -ErrorAction SilentlyContinue) }
function Ok($text)   { Write-Host "  ok       $text" }
function Miss($text) { Write-Host "  missing  $text" }

$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -ne 'AMD64') { Write-Host "There is a build for x64 Windows only; on $arch, set CODIST_FROM_SOURCE=1 to build with Go." }
$asset = 'codist-windows-x64.exe'

function FromSource {
  if (-not (Have go))  { throw 'No release to download and Go is not installed. Install Go from https://go.dev/dl, or wait for a release.' }
  if (-not (Have git)) { throw 'Building from source needs git.' }
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("codist-" + [guid]::NewGuid())
  Write-Host "Building Codist from source (branch $Branch)..."
  git clone -q --depth 1 -b $Branch "https://github.com/$Repo.git" "$tmp\codist"
  Push-Location "$tmp\codist"
  try { $env:CGO_ENABLED = '0'; go build -tags 'grammar_subset grammar_subset_java' -o $Exe . } finally { Pop-Location }
  Remove-Item -Recurse -Force $tmp
}

New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
if ($env:CODIST_FROM_SOURCE -eq '1') {
  FromSource
} else {
  $url = if ($Version -eq 'latest') { "https://github.com/$Repo/releases/latest/download/$asset" } else { "https://github.com/$Repo/releases/download/$Version/$asset" }
  Write-Host "Downloading $asset ($Version)..."
  try {
    Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $Exe
  } catch {
    Write-Host "No release found for $asset."
    FromSource
  }
}
Write-Host "Installed $Exe"

# Put the folder on the user's PATH, once.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not ($userPath -split ';' | Where-Object { $_ -eq $BinDir })) {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$BinDir", 'User')
  Write-Host "Added $BinDir to your PATH. Open a new terminal for it to take effect."
}

Write-Host ''
Write-Host 'Checking what Codist needs:'
if (Have git) { Ok "git $((git --version) -replace 'git version ', '')" } else { Miss 'git: install it from https://git-scm.com/download/win' }
if (Have java) { Ok 'java (optional; for checks that run code)' } else { Write-Host '  -        java not found (optional; only for checks that run code)' }
$modelUrl = if ($env:CODIST_MODEL_URL) { $env:CODIST_MODEL_URL } else { 'http://localhost:11434' }
try {
  Invoke-WebRequest -UseBasicParsing -TimeoutSec 3 -Uri "$modelUrl/api/tags" | Out-Null
  Ok "model server at $modelUrl"
} catch {
  Write-Host "  -        no model server at $modelUrl (optional; Codist still runs its code checks)"
  Write-Host '           to add one: install Ollama from https://ollama.com, then: ollama pull qwen2.5-coder:7b'
}
Write-Host ''
Write-Host 'Next: cd into a Java repository and run  codist'
Write-Host '      codist doctor  shows the same checks again.'
