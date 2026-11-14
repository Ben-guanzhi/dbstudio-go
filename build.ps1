<#
.SYNOPSIS
    Build, check, smoke-test and package dbstudio.

.DESCRIPTION
    One entry point for the actions this project repeats: the verification block
    (gofmt / build / vet / test), a development binary, a stripped production
    binary with checksums, a smoke launch, and cleanup.

    The Go environment is pinned to the cache directories beside this repository
    (.gopath, .gocache, .gotmp) when they exist -- the same ones the project has
    been built with -- so a run does not download a second toolchain or fill the
    user's global cache. Set DBSTUDIO_GO_ENV=0 to use the ambient Go environment
    instead.

    CGO stays off: mygo forbids cgo, and that also rules out -race, whose
    detector needs a C toolchain on Windows. Tests run without it.

.PARAMETER Action
    build    (default) development binary at build\dbstudio.exe
    run      build, then start the app and wait until its window closes
    release   stripped production binary in dist\ with a .sha256 beside it
    check     the verification block: gofmt -l, build, vet, test
    test      the test suite only
    smoke     build, launch for a few seconds, confirm it stays up, kill it
    clean     remove build\ and dist\
    all       check, then release

.PARAMETER Target
    windows (default), linux, darwin, or all -- for release only. Cross-built
    binaries cannot be run here; they are packaged for another machine.

.PARAMETER Version
    A version for the release file name. Defaults to 0.1.0-<yyyyMMdd-HHmm>.

.PARAMETER SmokeSeconds
    How long the smoke launch must stay alive. Defaults to 8.

.EXAMPLE
    .\build.ps1 check

.EXAMPLE
    .\build.ps1 release -Target all -Version 0.2.0

.EXAMPLE
    .\build.ps1 smoke
#>
[CmdletBinding()]
param(
    [ValidateSet('build', 'run', 'release', 'check', 'test', 'smoke', 'clean', 'all')]
    [string]$Action = 'build',

    [ValidateSet('windows', 'linux', 'darwin', 'all')]
    [string]$Target = 'windows',

    [string]$Version = '',

    [int]$SmokeSeconds = 8
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$AppDir = $PSScriptRoot
$RepoDir = Split-Path -Parent $AppDir
$BuildDir = Join-Path $AppDir 'build'
$DistDir = Join-Path $AppDir 'dist'
$AppName = 'dbstudio'

# --- environment -------------------------------------------------------------

# The project's own caches, when the repository carries them. They are what the
# verified builds used; DBSTUDIO_GO_ENV=0 opts out.
if ($env:DBSTUDIO_GO_ENV -ne '0') {
    $pairs = @{
        GOPATH   = '.gopath'
        GOCACHE  = '.gocache'
        GOTMPDIR = '.gotmp'
    }
    foreach ($name in $pairs.Keys) {
        $dir = Join-Path $RepoDir $pairs[$name]
        if (Test-Path $dir) {
            Set-Item -Path "env:$name" -Value $dir
        }
    }
    # GOPATH implies GOMODCACHE ($GOPATH\pkg\mod) unless one is already set;
    # leaving GOMODCACHE unset keeps a single module cache in play.
    if (-not $env:GOMODCACHE -and $env:GOPATH) {
        $modCache = Join-Path $env:GOPATH 'pkg\mod'
        if (Test-Path $modCache) {
            $env:GOMODCACHE = $modCache
        }
    }
    if (-not $env:GOPROXY) { $env:GOPROXY = 'https://goproxy.cn,direct' }
}
$env:CGO_ENABLED = '0'
# mygo's production builds omit the inspector; the dev binary keeps it, as
# `mygo dev` does, so the automation plugin can attach while developing.
$ProdTags = @('-tags', 'mygo_noinspector')
$ProdLdflags = '-s -w -X github.com/egoist/mygo.production=1'

# --- helpers -----------------------------------------------------------------

$script:StepNo = 0
$script:StepTimes = [System.Collections.Generic.List[object]]::new()
$script:StepStart = $null

function Write-Step([string]$Text) {
    $script:StepNo++
    Write-Host ''
    Write-Host ("[{0}] {1}" -f $script:StepNo, $Text) -ForegroundColor Cyan
    $script:StepStart = [System.Diagnostics.Stopwatch]::StartNew()
}

function Complete-Step {
    if ($script:StepStart) {
        $script:StepStart.Stop()
        $script:StepTimes.Add([pscustomobject]@{
                Step = $script:StepNo
                Ms   = [int]$script:StepStart.Elapsed.TotalMilliseconds
            })
        $script:StepStart = $null
    }
}

function Write-Ok([string]$Text) {
    Write-Host ("    OK  " + $Text) -ForegroundColor Green
    Complete-Step
}

function Fail([string]$Text) {
    Write-Host ''
    Write-Host ("FAILED: " + $Text) -ForegroundColor Red
    Complete-Step
    exit 1
}

# Invoke-Go runs `go` with the arguments given, in the app directory, and fails
# the script when the tool exits non-zero. The output goes to the console as it
# is produced, so a long test run shows its progress.
function Invoke-Go([string[]]$GoArgs) {
    & go @GoArgs
    if ($LASTEXITCODE -ne 0) {
        Fail ("go " + ($GoArgs -join ' ') + " (exit $LASTEXITCODE)")
    }
}

function Get-GoVersion {
    $v = & go version 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $v) { Fail 'go is not on PATH' }
    return $v
}

# New-Version is the release's default version: a date stamp, so two releases
# never collide, with a 0.1.0 base a person can read.
function New-Version {
    return '0.1.0-' + (Get-Date -Format 'yyyyMMdd-HHmm')
}

function Write-Env {
    Write-Host ("  go        : " + (Get-GoVersion))
    Write-Host ("  app dir   : " + $AppDir)
    Write-Host ("  GOPATH    : " + [string]$env:GOPATH)
    Write-Host ("  GOMODCACHE: " + [string]$env:GOMODCACHE)
    Write-Host ("  GOCACHE   : " + [string]$env:GOCACHE)
    Write-Host ("  CGO       : " + [string]$env:CGO_ENABLED)
}

# --- steps -------------------------------------------------------------------

function Step-Fmt {
    Write-Step 'gofmt -l (no file may be unformatted)'
    Push-Location $AppDir
    try {
        $bad = & gofmt -l .
        if ($LASTEXITCODE -ne 0) { Fail 'gofmt failed' }
        if ($bad) {
            Write-Host ($bad -join "`n") -ForegroundColor Yellow
            Fail 'unformatted files above; run: gofmt -w .'
        }
    }
    finally { Pop-Location }
    Write-Ok 'all files formatted'
}

function Step-Build {
    param(
        [string]$OutFile,
        [string[]]$ExtraArgs = @(),
        [string]$Goos = '',
        [string]$Goarch = ''
    )
    $savedGoos = $env:GOOS
    $savedGoarch = $env:GOARCH
    try {
        if ($Goos) { $env:GOOS = $Goos }
        if ($Goarch) { $env:GOARCH = $Goarch }
        $goArgs = @('build') + $ExtraArgs + @('-o', $OutFile, '.')
        Invoke-Go $goArgs
    }
    finally {
        $env:GOOS = $savedGoos
        $env:GOARCH = $savedGoarch
    }
}

function Step-Vet {
    Write-Step 'go vet (production tags)'
    Push-Location $AppDir
    try { Invoke-Go (@('vet') + $ProdTags + @('./...')) }
    finally { Pop-Location }
    Write-Ok 'vet clean'
}

function Step-Test {
    Write-Step 'go test (count=1, no cache)'
    Push-Location $AppDir
    try { Invoke-Go @('test', '-count=1', '-timeout', '300s', './...') }
    finally { Pop-Location }
    Write-Ok 'tests pass'
}

function Step-Clean {
    Write-Step 'clean build artefacts'
    foreach ($dir in @($BuildDir, $DistDir)) {
        if (Test-Path $dir) {
            Remove-Item $dir -Recurse -Force
            Write-Host ("    removed " + $dir)
        }
    }
    Write-Ok 'clean'
}

function Step-DevBuild {
    Write-Step 'build development binary'
    New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null
    $out = Join-Path $BuildDir ($AppName + '.exe')
    # The dev binary keeps the inspector (no mygo_noinspector), like `mygo dev`.
    Step-Build -OutFile $out
    $size = [math]::Round((Get-Item $out).Length / 1MB, 1)
    Write-Ok ($out + "  (" + $size + " MB)")
    return $out
}

function Step-Release {
    if (-not $Version) { $Version = New-Version }
    # The Windows exe's icon, version block and manifest come from a .syso
    # beside main: regenerate it when go-winres is on PATH (install with
    # `go install github.com/tc-hib/go-winres@latest`), keep the committed one
    # otherwise — the artwork only changes when resources/ does. 波 100.
    $winres = Get-Command 'go-winres' -ErrorAction SilentlyContinue
    if ($winres -and (Test-Path (Join-Path $AppDir 'winres\winres.json'))) {
        Write-Step 'windows resources (icon / version / manifest)'
        Push-Location $AppDir
        try {
            & go-winres simply --arch amd64 --out rsrc 2>&1 | ForEach-Object { Write-Host "    $_" }
        } finally {
            Pop-Location
        }
        Write-Ok 'rsrc_windows_amd64.syso'
    }
    $targets = switch ($Target) {
        'all' { @('windows', 'linux', 'darwin') }
        default { @($Target) }
    }
    New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
    $built = @()
    foreach ($os in $targets) {
        Write-Step ("release build: " + $os + "/amd64  (" + $Version + ")")
        $ext = if ($os -eq 'windows') { '.exe' } else { '' }
        $name = "$AppName-$Version-$os-amd64$ext"
        $out = Join-Path $DistDir $name
        # -trimpath drops this machine's paths; -s -w strip symbols; production=1
        # turns mygo's dev mode off; -X main.version stamps the release (the
        # About box shows it, an unversioned build says "dev").
        $ldflags = "$ProdLdflags -X main.version=$Version"
        Step-Build -OutFile $out -Goos $os -Goarch 'amd64' `
            -ExtraArgs (@('-trimpath') + $ProdTags + @('-ldflags', $ldflags))
        $size = [math]::Round((Get-Item $out).Length / 1MB, 1)
        $hash = (Get-FileHash -Algorithm SHA256 -Path $out).Hash.ToLowerInvariant()
        "$hash  $name" | Set-Content -Path ($out + '.sha256') -Encoding ascii -NoNewline
        Write-Ok ("$name  (" + $size + " MB)  sha256 " + $hash.Substring(0, 12) + '...')
        $built += $out
    }
    return $built
}

function Step-Smoke {
    $exe = Join-Path $BuildDir ($AppName + '.exe')
    if (-not (Test-Path $exe)) { $exe = Step-DevBuild }
    Write-Step ("smoke launch (" + $SmokeSeconds + "s)")
    $outLog = Join-Path $BuildDir 'smoke.out.txt'
    $errLog = Join-Path $BuildDir 'smoke.err.txt'
    foreach ($f in @($outLog, $errLog)) {
        if (Test-Path $f) { Remove-Item $f -Force }
    }
    $proc = Start-Process -FilePath $exe -PassThru -NoNewWindow `
        -RedirectStandardOutput $outLog -RedirectStandardError $errLog
    try {
        Start-Sleep -Seconds $SmokeSeconds
        $alive = $null -ne (Get-Process -Id $proc.Id -ErrorAction SilentlyContinue)
        $stderr = ''
        if (Test-Path $errLog) { $stderr = (Get-Content $errLog -Raw) }
        if (-not $alive) {
            Fail ("the app exited within " + $SmokeSeconds + "s; stderr: " + $stderr)
        }
        if ($stderr -and $stderr.Trim()) {
            Write-Host '    stderr was not empty:' -ForegroundColor Yellow
            Write-Host $stderr -ForegroundColor Yellow
            Fail 'the app wrote to stderr'
        }
        Write-Ok ("still running after " + $SmokeSeconds + "s, stderr empty")
    }
    finally {
        if (-not $proc.HasExited) {
            Stop-Process -Id $proc.Id -Force
            # A force-killed child may hold its redirected files for a moment;
            # wait for the exit before removing them.
            $null = $proc.WaitForExit(5000)
        }
        foreach ($f in @($outLog, $errLog)) {
            for ($try = 0; $try -lt 12; $try++) {
                if (-not (Test-Path $f)) { break }
                try {
                    Remove-Item $f -Force -ErrorAction Stop
                    break
                }
                catch {
                    Start-Sleep -Milliseconds 100
                }
            }
            if (Test-Path $f) {
                Write-Host ("    note: could not remove " + (Split-Path -Leaf $f) + " (still open)") -ForegroundColor Yellow
            }
        }
    }
}

function Write-Summary {
    Write-Host ''
    Write-Host 'summary' -ForegroundColor Cyan
    foreach ($s in $script:StepTimes) {
        Write-Host ("  step {0}: {1} ms" -f $s.Step, $s.Ms)
    }
}

# --- main --------------------------------------------------------------------

Push-Location $AppDir
try {
    Write-Host ("dbstudio build * action=" + $Action) -ForegroundColor White
    Write-Env

    switch ($Action) {
        'build' {
            $null = Step-DevBuild
        }
        'run' {
            $exe = Step-DevBuild
            Write-Step 'start the app (closes the step when the window closes)'
            & $exe
            Write-Ok 'app exited'
        }
        'release' {
            $null = Step-Release
        }
        'check' {
            Step-Fmt
            Write-Step 'go build ./...'
            Invoke-Go @('build', './...')
            Write-Ok 'build clean'
            Step-Vet
            Step-Test
        }
        'test' {
            Step-Test
        }
        'smoke' {
            Step-Smoke
        }
        'clean' {
            Step-Clean
        }
        'all' {
            Step-Fmt
            Write-Step 'go build ./...'
            Invoke-Go @('build', './...')
            Write-Ok 'build clean'
            Step-Vet
            Step-Test
            $null = Step-Release
        }
    }

    Write-Summary
    Write-Host ''
    Write-Host 'done' -ForegroundColor Green
}
finally {
    Pop-Location
}
