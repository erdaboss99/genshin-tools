<#
.SYNOPSIS
    genshin-tools build automation for Windows, mirroring the Makefile.

.DESCRIPTION
    Windows has no make, so this script provides the same targets. Run it from the
    repository root:

        .\make.ps1            # list targets
        .\make.ps1 build      # build bin\genshin-tools.exe
        .\make.ps1 check build

    If scripts are blocked, run it as:

        powershell -ExecutionPolicy Bypass -File .\make.ps1 build

    Works in Windows PowerShell 5.1 and PowerShell 7.
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0, ValueFromRemainingArguments = $true)]
    [string[]] $Targets = @('help')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'

$Binary = 'genshin-tools'
$Package = "./cmd/$Binary"
$BinDir = 'bin'
$CoverFile = 'coverage.out'
# The scanner and its libraries. tools/genfixtures is left out because checking it
# needs its dependency downloaded; "fixtures" builds it when it is needed.
$Packages = @('./...', './control/...', './ocr/...')
$Modules = @('.', 'control', 'ocr')

Set-Location -LiteralPath $PSScriptRoot

# Runs a native command and stops the script if it fails.
function Invoke-Native {
    param([Parameter(Mandatory)] [string] $Command, [string[]] $Arguments = @())
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

# Runs a native command and returns its output lines, stopping the script if it fails.
function Get-NativeOutput {
    param([Parameter(Mandatory)] [string] $Command, [string[]] $Arguments = @())
    $output = & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
    @($output | Where-Object { $_ -ne '' })
}

function Get-Version {
    $version = & git describe --tags --always --dirty 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $version) { return 'dev' }
    "$version".Trim()
}

# Target name -> description, in display order.
$Help = [ordered]@{
    'help'        = 'Show this help'
    'all'         = 'Run all checks and build'
    'build'       = 'Build bin\genshin-tools.exe'
    'test'        = 'Run tests (real-OCR tests are skipped when Tesseract is missing)'
    'cover'       = 'Run tests and print total coverage'
    'vet'         = 'Run go vet'
    'fmt'         = 'Format all Go code'
    'fmt-check'   = 'Fail if any Go file needs formatting'
    'deps-check'  = 'Fail if the scanner or its libraries depend on anything unexpected'
    'tidy'        = "Tidy every module's go.mod"
    'check'       = 'Run every check'
    'update-data' = 'Refresh the reference data embedded in the binary'
    'fixtures'    = 'Regenerate OCR test images'
    'clean'       = 'Remove build output'
}

function Target-help {
    Write-Host 'Usage: .\make.ps1 <target> [<target>...]'
    foreach ($name in $Help.Keys) {
        Write-Host ('  ' + $name.PadRight(16)) -ForegroundColor Cyan -NoNewline
        Write-Host " $($Help[$name])"
    }
}

function Target-all {
    Target-check
    Target-build
}

function Target-build {
    $ldflags = "-s -w -X main.version=$(Get-Version)"
    Invoke-Native go @('build', '-trimpath', "-ldflags=$ldflags", '-o', "$BinDir/$Binary.exe", $Package)
}

function Target-test {
    Invoke-Native go (@('test') + $Packages)
}

function Target-cover {
    Invoke-Native go (@('test', "-coverprofile=$CoverFile") + $Packages)
    $summary = @(Get-NativeOutput go @('tool', 'cover', "-func=$CoverFile"))
    $summary[-1]
}

function Target-vet {
    Invoke-Native go (@('vet') + $Packages)
}

function Target-fmt {
    Invoke-Native gofmt @('-s', '-w', '.')
}

function Target-fmt-check {
    $unformatted = @(Get-NativeOutput gofmt @('-s', '-l', '.'))
    if ($unformatted.Count -gt 0) {
        Write-Host 'Needs gofmt:'
        $unformatted | ForEach-Object { Write-Host $_ }
        throw 'fmt-check failed'
    }
}

function Target-deps-check {
    $failed = $false
    foreach ($module in $Modules) {
        Push-Location -LiteralPath $module
        try {
            $previous = $env:GOWORK
            $env:GOWORK = 'off'
            try { $path = @(Get-NativeOutput go @('list', '-m'))[0] } finally { $env:GOWORK = $previous }

            # The root module may import its own subpackages; libraries must import nothing.
            if ($module -eq '.') { $allowed = '^' + [regex]::Escape($path) }
            else { $allowed = '^' + [regex]::Escape($path) + '$' }

            $deps = @(Get-NativeOutput go @('list', '-deps', '-f', '{{if not .Standard}}{{.ImportPath}}{{end}}', './...') |
                Where-Object { $_ -notmatch $allowed })
            if ($deps.Count -gt 0) {
                Write-Host "$path has unexpected dependencies:"
                $deps | ForEach-Object { Write-Host $_ }
                $failed = $true
            }
        }
        finally { Pop-Location }
    }
    if ($failed) { throw 'deps-check failed' }
}

function Target-tidy {
    $previous = $env:GOWORK
    $env:GOWORK = 'off'
    try {
        foreach ($module in $Modules + 'tools/genfixtures') {
            Push-Location -LiteralPath $module
            try { Invoke-Native go @('mod', 'tidy') } finally { Pop-Location }
        }
    }
    finally { $env:GOWORK = $previous }
}

function Target-check {
    Target-fmt-check
    Target-vet
    Target-deps-check
    Target-test
}

function Target-update-data {
    Invoke-Native go @('run', $Package, 'update-data', '-o', 'internal/catalog/data')
}

function Target-fixtures {
    Invoke-Native go @('run', './tools/genfixtures', '-o', 'internal/scan/testdata')
}

function Target-clean {
    foreach ($item in @($BinDir, $CoverFile)) {
        if (Test-Path -LiteralPath $item) { Remove-Item -LiteralPath $item -Recurse -Force }
    }
}

foreach ($target in $Targets) {
    if (-not $Help.Contains($target)) {
        Target-help
        Write-Host "make.ps1: unknown target: $target" -ForegroundColor Red
        exit 2
    }
}

try {
    foreach ($target in $Targets) { & "Target-$target" }
}
catch {
    Write-Host "make.ps1: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
