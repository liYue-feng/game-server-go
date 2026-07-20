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
    It 'accepts checkout line endings and checks both client outputs' {
        $backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
        $backendParent = Split-Path $backendRoot -Parent
        $workspaceRoot = Split-Path (Split-Path $backendParent -Parent) -Parent
        $worktreeName = Split-Path $backendRoot -Leaf
        $clientRoot = (Resolve-Path (Join-Path $workspaceRoot "game-client-unity\.worktrees\$worktreeName")).Path
        $csharpOutputs = @(
            (Join-Path $clientRoot 'tools\protobuf\generated\Messages.cs'),
            (Join-Path $clientRoot 'Assets\Scripts\Protocol\Generated\Messages.cs')
        )
        $originalContents = @{}

        try {
            foreach ($output in $csharpOutputs) {
                $originalContents[$output] = [IO.File]::ReadAllBytes($output)
                $normalizedText = [IO.File]::ReadAllText($output).Replace("`r`n", "`n").Replace("`r", "`n")
                [IO.File]::WriteAllText($output, $normalizedText.Replace("`n", "`r`n"))
            }

            { & $scriptPath -ClientRoot $clientRoot -Check } | Should Not Throw
        }
        finally {
            foreach ($output in $csharpOutputs) {
                [IO.File]::WriteAllBytes($output, $originalContents[$output])
            }
        }
    }
}
