$scriptPath = Join-Path $PSScriptRoot 'Generate-Protocol.ps1'

function Get-RawSha256([string]$Path) {
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash
}

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
. (Join-Path $PSScriptRoot 'PeerRootResolver.ps1')
$projectParent = Split-Path $projectRoot -Parent
$isWorktree = (Split-Path $projectParent -Leaf) -eq '.worktrees'
$explicitClientRoot = if ($isWorktree) { $null } else { Join-Path $projectParent 'game-client-unity' }
$clientRoot = Resolve-PeerRepositoryRoot -CurrentRoot $projectRoot -ExplicitPeerRoot $explicitClientRoot `
    -PeerRepositoryName 'game-client-unity' -PeerDescription 'client'

Describe 'Canonical schema ownership' {
    It 'owns one local game.proto and rejects the old source names' {
        $legacySchema = Join-Path $projectRoot ('proto\game\v1\' + 'messages' + '.proto')
        $legacyGenerated = Join-Path $projectRoot ('internal\protocolpb\' + 'messages' + '.pb.go')

        Test-Path (Join-Path $projectRoot 'proto\game.proto') | Should Be $true
        @(Get-ChildItem (Join-Path $projectRoot 'proto') -Recurse -Filter '*.proto').Count | Should Be 1
        Test-Path $legacySchema | Should Be $false
        Test-Path (Join-Path $projectRoot 'internal\protocolpb\game.pb.go') | Should Be $true
        Test-Path $legacyGenerated | Should Be $false
    }

    It 'matches the sibling client schema byte-for-byte' {
        $serverSchema = Join-Path $projectRoot 'proto\game.proto'
        $clientSchema = Join-Path $clientRoot 'proto\game.proto'
        Test-Path -LiteralPath $serverSchema -PathType Leaf | Should Be $true
        Test-Path -LiteralPath $clientSchema -PathType Leaf | Should Be $true
        (Get-RawSha256 -Path $serverSchema) | Should Be (Get-RawSha256 -Path $clientSchema)
    }
}

Describe 'Generate-Protocol toolchain cache verification' {
    $originalTemp = $env:TEMP
    $originalTmp = $env:TMP
    $isolatedTemp = Join-Path ([System.IO.Path]::GetTempPath()) ("protobuf-cache-test-" + [Guid]::NewGuid().ToString('N'))

    BeforeEach {
        New-Item -ItemType Directory -Force -Path (Join-Path $isolatedTemp 'game-protobuf-toolchain') | Out-Null
        Set-Content -LiteralPath (Join-Path $isolatedTemp 'game-protobuf-toolchain\protoc-35.0-win64.zip') -Value 'corrupted cached compiler archive' -NoNewline
        $env:TEMP = $isolatedTemp
        $env:TMP = $isolatedTemp
    }

    AfterEach {
        $env:TEMP = $originalTemp
        $env:TMP = $originalTmp
        Remove-Item -LiteralPath $isolatedTemp -Recurse -Force -ErrorAction SilentlyContinue
    }

    It 'rejects a corrupted cached protoc archive before generation' {
        $failure = $null
        try {
            & $scriptPath -Check
        }
        catch {
            $failure = $_
        }

        $failure | Should Not BeNullOrEmpty
        $failure.Exception.Message | Should Match 'SHA256 mismatch'
    }
}

Describe 'Generate-Protocol checked-in output' {
    $fixtureRoot = $null
    $fixtureScript = $null
    $fixtureGeneratedPath = $null

    BeforeEach {
        $fixtureRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("protobuf-output-test-" + [Guid]::NewGuid().ToString('N'))
        $fixtureScript = Join-Path $fixtureRoot 'tools\protobuf\Generate-Protocol.ps1'
        $fixtureSchemaPath = Join-Path $fixtureRoot 'proto\game.proto'
        $fixtureGeneratedPath = Join-Path $fixtureRoot 'internal\protocolpb\game.pb.go'

        foreach ($path in @($fixtureScript, $fixtureSchemaPath, $fixtureGeneratedPath)) {
            New-Item -ItemType Directory -Force -Path (Split-Path $path -Parent) | Out-Null
        }
        Copy-Item -LiteralPath $scriptPath -Destination $fixtureScript
        Copy-Item -LiteralPath (Join-Path $projectRoot 'proto\game.proto') -Destination $fixtureSchemaPath
        Copy-Item -LiteralPath (Join-Path $projectRoot 'internal\protocolpb\game.pb.go') -Destination $fixtureGeneratedPath
    }

    AfterEach {
        Remove-Item -LiteralPath $fixtureRoot -Recurse -Force -ErrorAction SilentlyContinue
    }

    It 'accepts checkout line endings for the Go output' {
        $normalizedText = [IO.File]::ReadAllText($fixtureGeneratedPath).Replace("`r`n", "`n").Replace("`r", "`n")
        [IO.File]::WriteAllText($fixtureGeneratedPath, $normalizedText.Replace("`n", "`r`n"))

        { & $fixtureScript -Check } | Should Not Throw
    }

    It 'rejects a standalone carriage return in the Go output' {
        $text = [IO.File]::ReadAllText($fixtureGeneratedPath).Replace("`r`n", "`n").Replace("`r", "`n")
        $firstLineFeed = $text.IndexOf("`n")
        $firstLineFeed | Should BeGreaterThan -1
        $bareCarriageReturn = $text.Substring(0, $firstLineFeed) + "`r" + $text.Substring($firstLineFeed + 1)
        [IO.File]::WriteAllText($fixtureGeneratedPath, $bareCarriageReturn, (New-Object Text.UTF8Encoding($false)))

        { & $fixtureScript -Check } | Should Throw
    }

    It 'rejects a UTF-8 BOM in the Go output' {
        $content = [IO.File]::ReadAllBytes($fixtureGeneratedPath)
        $withBom = New-Object byte[] ($content.Length + 3)
        $withBom[0] = 0xEF
        $withBom[1] = 0xBB
        $withBom[2] = 0xBF
        [Array]::Copy($content, 0, $withBom, 3, $content.Length)
        [IO.File]::WriteAllBytes($fixtureGeneratedPath, $withBom)

        { & $fixtureScript -Check } | Should Throw
    }

    It 'reports semantic drift in the Go output' {
        Add-Content -LiteralPath $fixtureGeneratedPath -Value '// semantic drift'

        $failure = $null
        try {
            & $fixtureScript -Check
        }
        catch {
            $failure = $_
        }

        $failure | Should Not BeNullOrEmpty
        $failure.Exception.Message | Should Match ([regex]::Escape($fixtureGeneratedPath))
    }
}
