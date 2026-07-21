[CmdletBinding()]
param(
    [string]$ProtocPath,
    [switch]$Check
)

$ErrorActionPreference = 'Stop'

$ProtocVersion = '35.0'
$ProtocArchive = "protoc-$ProtocVersion-win64.zip"
$ProtocUrl = "https://github.com/protocolbuffers/protobuf/releases/download/v$ProtocVersion/$ProtocArchive"
$ProtocSha256 = 'D1CEDE9E308CC3EB072392AF1C02CCAE4BDD3D2F374EC2970DBD8CDFDAA91363'
$GoGeneratorVersion = 'v1.36.11'

$backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$schemaPath = Join-Path $backendRoot 'proto\game.proto'
$goOutputPath = Join-Path $backendRoot 'internal\protocolpb\game.pb.go'
$toolchainRoot = Join-Path $env:TEMP 'game-protobuf-toolchain'

function Assert-FileHash {
    param([string]$Path, [string]$Expected, [string]$Description)

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash
    if ($actual -ne $Expected) {
        throw "SHA256 mismatch for $Description at '$Path': expected $Expected, got $actual. Delete the cached file and rerun generation."
    }
}

function Get-PinnedProtoc {
    param([string]$RequestedPath)

    if (-not [string]::IsNullOrWhiteSpace($RequestedPath)) {
        return (Resolve-Path $RequestedPath).Path
    }

    $path = Join-Path $toolchainRoot "protoc-$ProtocVersion\bin\protoc.exe"
    $archivePath = Join-Path $toolchainRoot $ProtocArchive
    New-Item -ItemType Directory -Force -Path $toolchainRoot | Out-Null
    if (-not (Test-Path -LiteralPath $archivePath)) {
        Invoke-WebRequest -UseBasicParsing -Uri $ProtocUrl -OutFile $archivePath
    }
    Assert-FileHash -Path $archivePath -Expected $ProtocSha256 -Description "protoc $ProtocVersion archive"
    if (-not (Test-Path -LiteralPath $path)) {
        Expand-Archive -LiteralPath $archivePath -DestinationPath (Split-Path $path -Parent | Split-Path -Parent) -Force
    }
    return $path
}

function Get-NormalizedGeneratedFingerprint {
    param([string]$Path)

    [byte[]]$source = [IO.File]::ReadAllBytes($Path)
    $normalized = New-Object 'System.Collections.Generic.List[byte]'
    for ($index = 0; $index -lt $source.Length; $index++) {
        if ($source[$index] -eq 0x0D -and $index + 1 -lt $source.Length -and $source[$index + 1] -eq 0x0A) {
            continue
        }
        $normalized.Add($source[$index])
    }

    return [Convert]::ToBase64String($normalized.ToArray())
}

function Assert-CommandVersion {
    param([string]$Command, [string]$Expected)

    $version = (& $Command --version 2>&1 | Out-String).Trim()
    if ($version -ne $Expected) {
        throw "$Command version mismatch: expected '$Expected', got '$version'."
    }
}

function Ensure-GoGenerator {
    $generatorPath = Join-Path $toolchainRoot "bin\protoc-gen-go.exe"
    if (-not (Test-Path -LiteralPath $generatorPath)) {
        New-Item -ItemType Directory -Force -Path (Split-Path $generatorPath -Parent) | Out-Null
        $previousGoBin = $env:GOBIN
        try {
            $env:GOBIN = Split-Path $generatorPath -Parent
            go install "google.golang.org/protobuf/cmd/protoc-gen-go@$GoGeneratorVersion"
        }
        finally {
            $env:GOBIN = $previousGoBin
        }
    }
    $generatorExpectedVersion = "$(Split-Path $generatorPath -Leaf) $GoGeneratorVersion"
    Assert-CommandVersion -Command $generatorPath -Expected $generatorExpectedVersion
    return $generatorPath
}

function Copy-OrCheckGeneratedFile {
    param([string]$Candidate, [string]$Committed)

    if ($Check) {
        if (-not (Test-Path -LiteralPath $Committed)) {
            throw "Generated file is missing: $Committed"
        }
        if ((Get-NormalizedGeneratedFingerprint -Path $Candidate) -cne (Get-NormalizedGeneratedFingerprint -Path $Committed)) {
            throw "Generated file differs from the checked-in output: $Committed"
        }
        return
    }

    New-Item -ItemType Directory -Force -Path (Split-Path $Committed -Parent) | Out-Null
    Copy-Item -LiteralPath $Candidate -Destination $Committed -Force
}

if (-not (Test-Path -LiteralPath $schemaPath)) {
    throw "Canonical schema is missing: $schemaPath"
}

$protoc = Get-PinnedProtoc -RequestedPath $ProtocPath
Assert-CommandVersion -Command $protoc -Expected "libprotoc $ProtocVersion"
$generator = Ensure-GoGenerator
$temporaryRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("game-protocol-" + [Guid]::NewGuid().ToString('N'))

try {
    New-Item -ItemType Directory -Force -Path $temporaryRoot | Out-Null
    $generatorDirectory = Split-Path $generator -Parent
    $env:PATH = "$generatorDirectory$([IO.Path]::PathSeparator)$env:PATH"
    & $protoc "--proto_path=$(Join-Path $backendRoot 'proto')" "--go_out=$temporaryRoot" '--go_opt=module=game-server' 'game.proto'
    if ($LASTEXITCODE -ne 0) { throw 'protoc Go generation failed.' }

    Copy-OrCheckGeneratedFile -Candidate (Join-Path $temporaryRoot 'internal\protocolpb\game.pb.go') -Committed $goOutputPath
}
finally {
    Remove-Item -LiteralPath $temporaryRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Output "protoc=$ProtocVersion protoc-gen-go=$GoGeneratorVersion"
