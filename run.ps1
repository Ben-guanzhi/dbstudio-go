# Sets up a self-contained Go environment inside the workspace, then runs the app.
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $MyInvocation.MyCommand.Path

$env:GOPATH   = 'D:\tools\db\.gopath'
$env:GOMODCACHE = 'D:\tools\db\.gopath\pkg\mod'
$env:GOCACHE  = 'D:\tools\db\.gocache'
$env:GOTMPDIR = 'D:\tools\db\.gotmp'

foreach ($d in '.gopath', '.gocache', '.gotmp') {
    New-Item -ItemType Directory -Force -Path (Join-Path 'D:\tools\db' $d) | Out-Null
}

Push-Location $root
try {
    go run .
} finally {
    Pop-Location
}
