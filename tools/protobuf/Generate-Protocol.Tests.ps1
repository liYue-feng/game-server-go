$scriptPath = Join-Path $PSScriptRoot 'Generate-Protocol.ps1'

function Get-RawSha256([string]$Path) {
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash
}

function Test-ContainsStaleTransportDescription([string]$Content) {
    foreach ($line in [regex]::Split($Content, '\r?\n')) {
        if ($line -match '(?i)6[- ]bytes?|six[- ]bytes?|6\s*\u5B57\u8282|\u516D\u5B57\u8282|Length\s*=\s*6\s*\+') {
            return $true
        }
        if ($line -match '(?i)Length.*MsgID' -and $line -notmatch '(?i)Seq') {
            return $true
        }

        $compact = [regex]::Replace($line, '\s+', '')
        $unsequencedTwoFieldFrame = '(?i)(?:4B|4bytes?|4\u5B57\u8282)(?:Length|(?:\u603B)?\u957F\u5EA6)\+(?:2B|2bytes?|2\u5B57\u8282)(?:MsgID|MessageID|\u6D88\u606F(?:ID|\u7F16\u53F7))'
        if ($compact -notmatch '(?i)(?:Seq|\u5E8F\u5217)' -and $compact -match $unsequencedTwoFieldFrame) {
            return $true
        }
    }

    return $false
}

function Test-ComposeKeepsPaymentPortDisabled([string]$Content) {
    return $Content -notmatch '(?<!\d)8081(?!\d)'
}

function Test-PaymentLineHasSensitiveTopic([string]$Line) {
    $hasCallbackOrListener = $Line -match '(?i)\bcallbacks?\b|\blisteners?\b'
    $hasPaymentQualifier = $Line -match '(?i)\bpayments?\b'
    $hasStandalonePaymentPort = $Line -match '(?<!\d)8081(?!\d)'
    if ($hasCallbackOrListener -and ($hasPaymentQualifier -or $hasStandalonePaymentPort)) {
        return $true
    }
    if ($hasPaymentQualifier -and
        $Line -match '(?i)\b(?:fulfill(?:s|ed|ing|ment)?|deliver(?:s|ed|ing|y)?|grant(?:s|ed|ing)?)\b') {
        return $true
    }
    if ($Line -match '\u652F\u4ED8\u56DE\u8C03') {
        return $true
    }
    if ($Line -match '\u652F\u4ED8' -and
        $Line -match '(?:\u76D1\u542C|\u53D1\u653E|\u53D1\u8D27|\u6388\u4E88)') {
        return $true
    }

    return $false
}

function Test-PaymentLineHasNegativeMarker([string]$Line) {
    if ($Line -match "(?i)\b(?:disabled|not|no|never|without|won't|isn't|aren't)\b") {
        return $true
    }
    if ($Line -match '(?:\u4E0D|\u672A|\u65E0|\u7981\u7528|\u6CA1\u6709)') {
        return $true
    }

    return $false
}

function Test-ContainsActivePaymentClaim([string]$Content) {
    foreach ($line in [regex]::Split($Content, '\r?\n')) {
        if ((Test-PaymentLineHasSensitiveTopic -Line $line) -and
            -not (Test-PaymentLineHasNegativeMarker -Line $line)) {
            return $true
        }
    }

    return $false
}

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
. (Join-Path $PSScriptRoot 'PeerRootResolver.ps1')
$projectParent = Split-Path $projectRoot -Parent
$isWorktree = (Split-Path $projectParent -Leaf) -eq '.worktrees'
$explicitClientRoot = if ($isWorktree) { $null } else { Join-Path $projectParent 'game-client-unity' }
$clientRoot = Resolve-PeerRepositoryRoot -CurrentRoot $projectRoot -ExplicitPeerRoot $explicitClientRoot `
    -PeerRepositoryName 'game-client-unity' -PeerDescription 'client'

$transportContract = 'Transport contract: 10-byte little-endian [Length uint32][MsgID uint16][Seq uint32]; Length includes the 10-byte header; request seq is nonzero; responses and errors echo the exact request seq; pushes use seq 0; Body is protobuf binary.'

Describe 'Authoritative transport documentation' {
    foreach ($relativePath in @('AGENTS.md', 'CLAUDE.md', 'README.md', 'internal/transport/connection.go')) {
        It "documents the sequenced protobuf frame in $relativePath" {
            $content = [IO.File]::ReadAllText((Join-Path $projectRoot $relativePath))
            $content | Should Match ([regex]::Escape($transportContract))
            (Test-ContainsStaleTransportDescription -Content $content) | Should Be $false
        }
    }

    foreach ($case in @(
        @{ Name = 'English words'; Content = 'Frame header: 4 bytes length + 2 bytes message ID.' },
        @{ Name = 'spaced Chinese abbreviations'; Content = [regex]::Unescape('\u5E27\u5934\uFF1A4B \u957F\u5EA6 + 2B \u6D88\u606F ID\u3002') },
        @{ Name = 'full Chinese units'; Content = [regex]::Unescape('\u5E27\u5934\uFF1A4 \u5B57\u8282\u957F\u5EA6 + 2 \u5B57\u8282\u6D88\u606F ID\u3002') }
    )) {
        It "rejects the $($case.Name) unsequenced frame description" {
            (Test-ContainsStaleTransportDescription -Content $case.Content) | Should Be $true
        }
    }

    It 'allows a complete sequenced frame description' {
        (Test-ContainsStaleTransportDescription -Content 'Frame header: 4B Length + 2B MsgID + 4B Seq.') | Should Be $false
    }

    It 'marks payment protocol IDs as reserved while production payment is disabled' {
        $readme = [IO.File]::ReadAllText((Join-Path $projectRoot 'README.md'))
        $readme | Should Match 'Payment protocol IDs 5001-5003 are reserved; production payment is disabled\.'
        (Test-ContainsActivePaymentClaim -Content $readme) | Should Be $false
    }

    It 'does not publish the disabled payment callback port in Docker Compose' {
        $compose = [IO.File]::ReadAllText((Join-Path $projectRoot 'docker-compose.yml'))
        (Test-ComposeKeepsPaymentPortDisabled -Content $compose) | Should Be $true
    }

    foreach ($case in @(
        @{ Name = 'host IP binding'; Section = 'ports'; Value = '127.0.0.1:18081:8081' },
        @{ Name = 'remapped host port'; Section = 'ports'; Value = '18081:8081' },
        @{ Name = 'long syntax target'; Section = 'ports'; Value = 'target: 8081' },
        @{ Name = 'bare exposed port'; Section = 'expose'; Value = '8081' }
    )) {
        It "rejects the $($case.Name) Compose payment port form" {
            $fixture = @('services:', '  game-server:', "    $($case.Section):", "      - $($case.Value)") -join "`n"
            (Test-ComposeKeepsPaymentPortDisabled -Content $fixture) | Should Be $false
        }
    }

    foreach ($case in @(
        @{ Name = 'published payment host port'; Content = @('services:', '  game-server:', '    ports:', '      - 8081:8080') -join "`n" },
        @{ Name = 'host IP published payment port'; Content = @('services:', '  game-server:', '    ports:', '      - 127.0.0.1:8081:8080') -join "`n" },
        @{ Name = 'long syntax published payment port'; Content = @('services:', '  game-server:', '    ports:', '      - target: 8080', '        published: 8081') -join "`n" },
        @{ Name = 'inline ports payment token'; Content = @('services:', '  game-server:', '    ports: ["8081:8080"]') -join "`n" },
        @{ Name = 'inline expose payment token'; Content = @('services:', '  game-server:', '    expose: [8081]') -join "`n" },
        @{ Name = 'environment payment token'; Content = @('services:', '  game-server:', '    environment:', '      - PAYMENT_PORT=8081') -join "`n" },
        @{ Name = 'comment payment token'; Content = @('services:', '  game-server:', '    # retired port 8081') -join "`n" }
    )) {
        It "rejects the $($case.Name) Compose form" {
            (Test-ComposeKeepsPaymentPortDisabled -Content $case.Content) | Should Be $false
        }
    }

    It 'allows an 8080-only Compose port list' {
        $fixture = @('services:', '  game-server:', '    ports:', '      - "8080:8080"') -join "`n"
        (Test-ComposeKeepsPaymentPortDisabled -Content $fixture) | Should Be $true
    }

    It 'allows a larger number containing 18081' {
        $fixture = @('services:', '  game-server:', '    environment:', '      - PAYMENT_PORT=18081') -join "`n"
        (Test-ComposeKeepsPaymentPortDisabled -Content $fixture) | Should Be $true
    }

    foreach ($case in @(
        @{ Name = 'active callback'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u63A5\u53D7\u652F\u4ED8\u56DE\u8C03\u3002') },
        @{ Name = 'active listener'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u76D1\u542C\u652F\u4ED8\u56DE\u8C03\u7AEF\u53E3 8081\u3002') },
        @{ Name = 'active fulfillment'; Content = [regex]::Unescape('\u652F\u4ED8\u6210\u529F\u540E\u53D1\u653E\u5546\u54C1\u5E76\u63A8\u9001 PayResultNotify\u3002') },
        @{ Name = 'English active callback'; Content = 'The server accepts payment callbacks.' },
        @{ Name = 'English active listener'; Content = 'The server listens on port 8081 for payment callbacks.' },
        @{ Name = 'English active fulfillment'; Content = 'Successful payments grant the purchased entitlement.' },
        @{ Name = 'reviewer listener bypass'; Content = 'Production payment listener runs on port 8081.' },
        @{ Name = 'reviewer passive callback bypass'; Content = 'Payment callbacks are accepted by the server.' }
    )) {
        It "rejects the $($case.Name) README claim" {
            (Test-ContainsActivePaymentClaim -Content $case.Content) | Should Be $true
        }
    }

    foreach ($case in @(
        @{ Name = 'Chinese direct negation'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u4E0D\u63A5\u53D7\u652F\u4ED8\u56DE\u8C03\uFF0C\u4E5F\u4E0D\u76D1\u542C\u7AEF\u53E3 8081\u3002') },
        @{ Name = 'Chinese will-not negation'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u4E0D\u4F1A\u63A5\u53D7\u652F\u4ED8\u56DE\u8C03\uFF0C\u4E5F\u4E0D\u4F1A\u76D1\u542C\u7AEF\u53E3 8081\u3002') },
        @{ Name = 'Chinese no-longer negation'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u4E0D\u518D\u5904\u7406\u652F\u4ED8\u56DE\u8C03\uFF0C\u4E5F\u4E0D\u518D\u76D1\u542C\u7AEF\u53E3 8081\u3002') },
        @{ Name = 'Chinese pending negation'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u672A\u542F\u7528\u652F\u4ED8\u56DE\u8C03\u3002') },
        @{ Name = 'Chinese none negation'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u65E0\u652F\u4ED8\u56DE\u8C03\u76D1\u542C\u3002') },
        @{ Name = 'Chinese disabled negation'; Content = [regex]::Unescape('\u652F\u4ED8\u56DE\u8C03\u5DF2\u7981\u7528\u3002') },
        @{ Name = 'Chinese does-not-have negation'; Content = [regex]::Unescape('\u670D\u52A1\u7AEF\u6CA1\u6709\u652F\u4ED8\u56DE\u8C03\u76D1\u542C\u3002') },
        @{ Name = 'English explicit negation'; Content = 'The server does not accept payment callbacks. Payment callbacks and fulfillment are disabled. No payment callback listener is active.' },
        @{ Name = 'English contraction negation'; Content = "The server won't accept payment callbacks." },
        @{ Name = 'English do-not negation'; Content = 'We do not accept payment callbacks.' },
        @{ Name = 'English bare-not negation'; Content = 'Payment callbacks are not enabled.' },
        @{ Name = 'English never negation'; Content = 'The server never accepts payment callbacks.' },
        @{ Name = 'English will-not negation'; Content = 'The server will not accept payment callbacks.' },
        @{ Name = 'English is-not contraction'; Content = "The server isn't accepting payment callbacks." },
        @{ Name = 'English are-not contraction'; Content = "Payment callbacks aren't active." },
        @{ Name = 'English without negation'; Content = 'The server runs without payment callbacks.' },
        @{ Name = 'unrelated event listener'; Content = 'The event listener updates the combat UI.' },
        @{ Name = 'unrelated login callbacks'; Content = 'Login callbacks refresh the session token.' }
    )) {
        It "allows the $($case.Name) payment statement" {
            (Test-ContainsActivePaymentClaim -Content $case.Content) | Should Be $false
        }
    }

    It 'exposes only the game port in the production Dockerfile' {
        $dockerfile = [IO.File]::ReadAllText((Join-Path $projectRoot 'Dockerfile'))
        @([regex]::Matches($dockerfile, '(?im)^\s*EXPOSE[^\r\n]*')).Count | Should Be 1
        $dockerfile | Should Match '(?im)^\s*EXPOSE\s+8080\s*$'
    }

    It 'contains no active payment callback claim in the production Dockerfile' {
        $dockerfile = [IO.File]::ReadAllText((Join-Path $projectRoot 'Dockerfile'))
        $dockerfile | Should Not Match '(?im)^#.*(?:payment\s+callback|\u652F\u4ED8\u56DE\u8C03)'
    }
}

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

    It 'defines CombatResultReq field 5 as double survival_time' {
        $schema = [IO.File]::ReadAllText((Join-Path $projectRoot 'proto\game.proto'))
        $schema | Should Match '(?m)^\s*double\s+survival_time\s*=\s*5\s*;'
        $schema | Should Not Match '(?m)^\s*int64\s+duration_ms\s*=\s*5\s*;'
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
