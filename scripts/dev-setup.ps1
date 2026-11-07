# Sets up Codist for development on Windows: clones every repository side by side, ties the Go modules together
# with go.work, and installs the interface's packages. In PowerShell:
#
#   .\scripts\dev-setup.ps1 [folder]     (default: the folder that holds this checkout, or .\codist-dev)
#
# Needs git and Go 1.26 or later; Node 20 or later only to work on the interface.
param([string]$Dir)
$ErrorActionPreference = 'Stop'
$Owner = 'https://github.com/AbiXnash'
$Repos = 'codist', 'codist-java', 'codist-git', 'codist-model', 'codist-review', 'codist-behaviour', 'codist-ui', 'codist-eval'
function Have($name) { [bool](Get-Command $name -ErrorAction SilentlyContinue) }
if (-not (Have git)) { throw 'Install git first: https://git-scm.com/download/win' }
if (-not (Have go))  { throw 'Install Go 1.26 or later first: https://go.dev/dl' }

if (-not $Dir) {
  $here = Split-Path -Parent $PSScriptRoot
  if ((Test-Path "$here\go.mod") -and (Select-String -Quiet -Path "$here\go.mod" -Pattern 'module github.com/AbiXnash/codist$')) { $Dir = Split-Path -Parent $here }
  else { $Dir = Join-Path (Get-Location) 'codist-dev' }
}
New-Item -ItemType Directory -Force -Path $Dir | Out-Null
Set-Location $Dir
Write-Host "Working in $Dir"
foreach ($r in $Repos) {
  if (Test-Path "$r\.git") { Write-Host "  ${r}: already there" } else { git clone -q "$Owner/$r.git"; Write-Host "  ${r}: cloned" }
}
Set-Location codist
Remove-Item -Force -ErrorAction SilentlyContinue go.work, go.work.sum
go work init . ../codist-java ../codist-git ../codist-model ../codist-review ../codist-behaviour ../codist-ui
Write-Host 'Created codist\go.work'
if (Have npm) {
  Push-Location ..\codist-ui; npm install --no-audit --no-fund | Out-Null; Pop-Location
  Write-Host "Installed the interface's packages"
} else { Write-Host 'Node is not installed: fine unless you change the interface (codist-ui).' }
Write-Host ''
Write-Host "Check it all:  cd $Dir\codist; go test -tags 'grammar_subset grammar_subset_java' ./..."
Write-Host "Build it:      go build -tags 'grammar_subset grammar_subset_java' -o bin\codist.exe ."
