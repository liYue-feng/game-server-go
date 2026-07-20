$scriptPath = Join-Path $PSScriptRoot 'Generate-Protocol.ps1'

Describe 'Generate-Protocol toolchain cache verification' {
    $originalTemp = $env:TEMP
    $originalTmp = $env:TMP
    $isolatedTemp = Join-Path ([System.IO.Path]::GetTempPath()) ("protobuf-cache-test-" + [Guid]::NewGuid().ToString('N'))
    $clientRoot = Join-Path $isolatedTemp 'client'

    BeforeEach {
        New-Item -ItemType Directory -Force -Path $isolatedTemp | Out-Null
        New-Item -ItemType Directory -Force -Path $clientRoot | Out-Null
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
            & $scriptPath -ClientRoot $clientRoot -Check
        }
        catch {
            $failure = $_
        }

        $failure | Should Not BeNullOrEmpty
        $failure.Exception.Message | Should Match 'SHA256 mismatch'
    }
}

Describe 'Generate-Protocol checked-in outputs' {
    $backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
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
    $sourceClientRoot = $candidates | Where-Object { Test-Path -LiteralPath $_ -PathType Container } | Select-Object -First 1

    if ([string]::IsNullOrWhiteSpace($sourceClientRoot)) {
        throw 'A sibling game-client-unity checkout is required for generated output tests.'
    }

    $sourceClientRoot = (Resolve-Path $sourceClientRoot).Path
    $fixtureRoot = $null
    $fixtureClientRoot = $null

    BeforeEach {
        $fixtureRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("protobuf-output-test-" + [Guid]::NewGuid().ToString('N'))
        $fixtureClientRoot = Join-Path $fixtureRoot 'client'
        $relativeOutputs = @(
            'tools\protobuf\generated\Messages.cs',
            'Assets\Scripts\Protocol\Generated\Messages.cs'
        )

        foreach ($relativeOutput in $relativeOutputs) {
            $sourceOutput = Join-Path $sourceClientRoot $relativeOutput
            $fixtureOutput = Join-Path $fixtureClientRoot $relativeOutput
            New-Item -ItemType Directory -Force -Path (Split-Path $fixtureOutput -Parent) | Out-Null
            Copy-Item -LiteralPath $sourceOutput -Destination $fixtureOutput
        }
    }

    AfterEach {
        Remove-Item -LiteralPath $fixtureRoot -Recurse -Force -ErrorAction SilentlyContinue
    }

    It 'accepts checkout line endings and checks both client outputs' {
        $csharpOutputs = @(
            (Join-Path $fixtureClientRoot 'tools\protobuf\generated\Messages.cs'),
            (Join-Path $fixtureClientRoot 'Assets\Scripts\Protocol\Generated\Messages.cs')
        )

        foreach ($output in $csharpOutputs) {
            $normalizedText = [IO.File]::ReadAllText($output).Replace("`r`n", "`n").Replace("`r", "`n")
            [IO.File]::WriteAllText($output, $normalizedText.Replace("`n", "`r`n"))
        }

        { & $scriptPath -ClientRoot $fixtureClientRoot -Check } | Should Not Throw
    }

    It 'rejects a standalone carriage return in the staged generated output' {
        $stagedOutput = Join-Path $fixtureClientRoot 'tools\protobuf\generated\Messages.cs'
        $text = [IO.File]::ReadAllText($stagedOutput).Replace("`r`n", "`n").Replace("`r", "`n")
        $firstLineFeed = $text.IndexOf("`n")
        $firstLineFeed | Should BeGreaterThan -1
        $bareCarriageReturn = $text.Substring(0, $firstLineFeed) + "`r" + $text.Substring($firstLineFeed + 1)
        [IO.File]::WriteAllText($stagedOutput, $bareCarriageReturn, (New-Object Text.UTF8Encoding($false)))

        { & $scriptPath -ClientRoot $fixtureClientRoot -Check } | Should Throw
    }

    It 'rejects a UTF-8 BOM in the staged generated output' {
        $stagedOutput = Join-Path $fixtureClientRoot 'tools\protobuf\generated\Messages.cs'
        $content = [IO.File]::ReadAllBytes($stagedOutput)
        $withBom = New-Object byte[] ($content.Length + 3)
        $withBom[0] = 0xEF
        $withBom[1] = 0xBB
        $withBom[2] = 0xBF
        [Array]::Copy($content, 0, $withBom, 3, $content.Length)
        [IO.File]::WriteAllBytes($stagedOutput, $withBom)

        { & $scriptPath -ClientRoot $fixtureClientRoot -Check } | Should Throw
    }

    It 'reports semantic drift in the Unity runtime output' {
        $runtimeOutput = Join-Path $fixtureClientRoot 'Assets\Scripts\Protocol\Generated\Messages.cs'
        Add-Content -LiteralPath $runtimeOutput -Value '// semantic drift'

        $failure = $null
        try {
            & $scriptPath -ClientRoot $fixtureClientRoot -Check
        }
        catch {
            $failure = $_
        }

        $failure | Should Not BeNullOrEmpty
        $failure.Exception.Message | Should Match ([regex]::Escape($runtimeOutput))
    }
}
