[CmdletBinding()]
param(
    [string]$ClientRoot,
    [string]$ProtocPath,
    [switch]$Check
)

$ErrorActionPreference = 'Stop'

$ProtocVersion = '35.0'
$ProtocArchive = "protoc-$ProtocVersion-win64.zip"
$ProtocUrl = "https://github.com/protocolbuffers/protobuf/releases/download/v$ProtocVersion/$ProtocArchive"
$ProtocSha256 = 'D1CEDE9E308CC3EB072392AF1C02CCAE4BDD3D2F374EC2970DBD8CDFDAA91363'
$GoGeneratorVersion = 'v1.36.11'
$GoogleProtobufVersion = '3.35.1'
$GoogleProtobufUrl = "https://api.nuget.org/v3-flatcontainer/google.protobuf/$GoogleProtobufVersion/google.protobuf.$GoogleProtobufVersion.nupkg"
$GoogleProtobufSha256 = '6BA51589915E3640E1FFDD384863DD0D73F0CA6A8AAC591EC81C42C6A3EE55CE'

$backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if ([string]::IsNullOrWhiteSpace($ClientRoot)) {
    $backendParent = Split-Path $backendRoot -Parent
    $isWorktree = (Split-Path $backendParent -Leaf) -eq '.worktrees'
    $workspaceRoot = if ($isWorktree) {
        Split-Path (Split-Path $backendParent -Parent) -Parent
    }
    else {
        $backendParent
    }
    $worktreeName = Split-Path $backendRoot -Leaf
    $candidates = if ($isWorktree) {
        @(
            (Join-Path $workspaceRoot "game-client-unity\.worktrees\$worktreeName"),
            (Join-Path $workspaceRoot 'game-client-unity')
        )
    }
    else {
        @((Join-Path $workspaceRoot 'game-client-unity'))
    }
    $ClientRoot = $candidates | Where-Object { Test-Path -LiteralPath $_ -PathType Container } | Select-Object -First 1
    if ([string]::IsNullOrWhiteSpace($ClientRoot)) {
        throw 'ClientRoot is required when no sibling game-client-unity checkout is available.'
    }
}
$ClientRoot = (Resolve-Path $ClientRoot).Path
$schemaPath = Join-Path $backendRoot 'proto\game\v1\messages.proto'
$goOutputPath = Join-Path $backendRoot 'internal\protocolpb\messages.pb.go'
$csharpOutputPath = Join-Path $ClientRoot 'tools\protobuf\generated\Messages.cs'
$csharpRuntimeOutputPath = Join-Path $ClientRoot 'Assets\Scripts\Protocol\Generated\Messages.cs'
$toolchainRoot = Join-Path $env:TEMP 'game-protobuf-toolchain'

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

function Assert-FileHash {
    param([string]$Path, [string]$Expected, [string]$Description)

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash
    if ($actual -ne $Expected) {
        throw "SHA256 mismatch for $Description at '$Path': expected $Expected, got $actual. Delete the cached file and rerun generation."
    }
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

function Ensure-GoogleProtobufNet45Source {
    $packagePath = Join-Path $toolchainRoot "Google.Protobuf.$GoogleProtobufVersion.nupkg"
    $extractPath = Join-Path $toolchainRoot "Google.Protobuf.$GoogleProtobufVersion"
    New-Item -ItemType Directory -Force -Path $toolchainRoot | Out-Null
    if (-not (Test-Path -LiteralPath $packagePath)) {
        Invoke-WebRequest -UseBasicParsing -Uri $GoogleProtobufUrl -OutFile $packagePath
    }
    Assert-FileHash -Path $packagePath -Expected $GoogleProtobufSha256 -Description "Google.Protobuf $GoogleProtobufVersion package"
    if (-not (Test-Path -LiteralPath (Join-Path $extractPath 'lib\net45\Google.Protobuf.dll'))) {
        $zipPath = "$packagePath.zip"
        Copy-Item -LiteralPath $packagePath -Destination $zipPath -Force
        Expand-Archive -LiteralPath $zipPath -DestinationPath $extractPath -Force
        Remove-Item -LiteralPath $zipPath -Force
    }
    if (-not (Test-Path -LiteralPath (Join-Path $extractPath 'lib\net45\Google.Protobuf.dll'))) {
        throw "Google.Protobuf $GoogleProtobufVersion does not include lib/net45/Google.Protobuf.dll."
    }
    return $packagePath
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
$packagePath = Ensure-GoogleProtobufNet45Source
$temporaryRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("game-protocol-" + [Guid]::NewGuid().ToString('N'))

try {
    New-Item -ItemType Directory -Force -Path $temporaryRoot | Out-Null
    $generatorDirectory = Split-Path $generator -Parent
    $env:PATH = "$generatorDirectory$([IO.Path]::PathSeparator)$env:PATH"
    & $protoc "--proto_path=$(Join-Path $backendRoot 'proto')" "--go_out=$temporaryRoot" '--go_opt=module=game-server' 'game/v1/messages.proto'
    if ($LASTEXITCODE -ne 0) { throw 'protoc Go generation failed.' }
    & $protoc "--proto_path=$(Join-Path $backendRoot 'proto\game\v1')" "--csharp_out=$temporaryRoot" 'messages.proto'
    if ($LASTEXITCODE -ne 0) { throw 'protoc C# generation failed.' }

    Copy-OrCheckGeneratedFile -Candidate (Join-Path $temporaryRoot 'internal\protocolpb\messages.pb.go') -Committed $goOutputPath
    Copy-OrCheckGeneratedFile -Candidate (Join-Path $temporaryRoot 'Messages.cs') -Committed $csharpOutputPath
    Copy-OrCheckGeneratedFile -Candidate (Join-Path $temporaryRoot 'Messages.cs') -Committed $csharpRuntimeOutputPath
}
finally {
    Remove-Item -LiteralPath $temporaryRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Output "protoc=$ProtocVersion protoc-gen-go=$GoGeneratorVersion Google.Protobuf=$GoogleProtobufVersion"
Write-Output "Google.Protobuf net45 source SHA256=$((Get-FileHash -Algorithm SHA256 -LiteralPath $packagePath).Hash)"
