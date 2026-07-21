[CmdletBinding()]
param([string]$ClientRoot)

$ErrorActionPreference = 'Stop'
$backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$schemaPath = Join-Path $backendRoot 'proto\game.proto'
$generatedGoPath = Join-Path $backendRoot 'internal\protocolpb\game.pb.go'

function Assert-Contains {
    param([string]$Content, [string]$Expected)
    if ($Content -notmatch [regex]::Escape($Expected)) {
        throw "Expected protocol contract text is absent: $Expected"
    }
}

function Get-RawSha256([string]$Path) {
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash
}

if (-not (Test-Path -LiteralPath $schemaPath -PathType Leaf)) {
    throw "Canonical schema is missing: $schemaPath"
}
$protoFiles = @(Get-ChildItem -LiteralPath (Join-Path $backendRoot 'proto') -Recurse -Filter '*.proto')
if ($protoFiles.Count -ne 1 -or $protoFiles[0].FullName -ne $schemaPath) {
    throw "Server must own only proto/game.proto: $($protoFiles.FullName -join ', ')"
}

$legacyGeneratedPath = Join-Path $backendRoot ('internal\protocolpb\' + 'messages' + '.pb.go')
if (Test-Path -LiteralPath $legacyGeneratedPath) {
    throw "Old generated Go source must be removed: $legacyGeneratedPath"
}

$schema = Get-Content -LiteralPath $schemaPath -Raw
@(
    'syntax = "proto3";',
    'package game.protocol.v1;',
    'option go_package = "game-server/internal/protocolpb;protocolpb";',
    'option csharp_namespace = "Game.Protocol";',
    'MESSAGE_ID_LOGIN_REQ = 1001;',
    'MESSAGE_ID_LOGIN_RESP = 1002;',
    'MESSAGE_ID_HEARTBEAT_REQ = 1003;',
    'MESSAGE_ID_HEARTBEAT_RESP = 1004;',
    'MESSAGE_ID_SAVE_ARCHIVE_REQ = 2001;',
    'MESSAGE_ID_SAVE_ARCHIVE_RESP = 2002;',
    'MESSAGE_ID_LOAD_ARCHIVE_REQ = 2003;',
    'MESSAGE_ID_LOAD_ARCHIVE_RESP = 2004;',
    'MESSAGE_ID_GET_RANK_REQ = 3001;',
    'MESSAGE_ID_GET_RANK_RESP = 3002;',
    'MESSAGE_ID_SUBMIT_SCORE_REQ = 3003;',
    'MESSAGE_ID_SUBMIT_SCORE_RESP = 3004;',
    'MESSAGE_ID_COMBAT_RESULT_REQ = 4001;',
    'MESSAGE_ID_COMBAT_RESULT_RESP = 4002;',
    'MESSAGE_ID_GET_ENEMY_CONFIGS_REQ = 4003;',
    'MESSAGE_ID_GET_ENEMY_CONFIGS_RESP = 4004;',
    'MESSAGE_ID_GET_DUNGEON_CONFIG_REQ = 4005;',
    'MESSAGE_ID_GET_DUNGEON_CONFIG_RESP = 4006;',
    'MESSAGE_ID_GET_STYLE_CONFIGS_REQ = 4007;',
    'MESSAGE_ID_GET_STYLE_CONFIGS_RESP = 4008;',
    'MESSAGE_ID_UNLOCK_STYLE_REQ = 4009;',
    'MESSAGE_ID_UNLOCK_STYLE_RESP = 4010;',
    'MESSAGE_ID_GET_PLAYER_STATS_REQ = 4011;',
    'MESSAGE_ID_GET_PLAYER_STATS_RESP = 4012;',
    'MESSAGE_ID_UPDATE_PLAYER_STATS_REQ = 4013;',
    'MESSAGE_ID_UPDATE_PLAYER_STATS_RESP = 4014;',
    'MESSAGE_ID_CREATE_ORDER_REQ = 5001;',
    'MESSAGE_ID_CREATE_ORDER_RESP = 5002;',
    'MESSAGE_ID_PAY_RESULT_NOTIFY = 5003;',
    'MESSAGE_ID_GM_COMMAND_REQ = 6001;',
    'MESSAGE_ID_GM_COMMAND_RESP = 6002;',
    'MESSAGE_ID_ERROR = 9999;',
    'message PlayerArchive {',
    'message ScoreMetadata {',
    'message CombatResultReq {',
    'int64 duration_ms = 5;',
    'BattleOutcome outcome = 7;',
    'string run_id = 7;',
    'bytes args_json = 2;'
) | ForEach-Object { Assert-Contains -Content $schema -Expected $_ }

if (-not (Test-Path -LiteralPath $generatedGoPath -PathType Leaf)) {
    throw "Generated Go protocol is missing: $generatedGoPath"
}
$generated = Get-Content -LiteralPath $generatedGoPath -Raw
Assert-Contains -Content $generated -Expected 'protoc-gen-go v1.36.11'

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
}

if (-not [string]::IsNullOrWhiteSpace($ClientRoot)) {
    $ClientRoot = (Resolve-Path $ClientRoot).Path
    $clientSchemaPath = Join-Path $ClientRoot 'proto\game.proto'
    if (-not (Test-Path -LiteralPath $clientSchemaPath -PathType Leaf)) {
        throw "Sibling client schema is missing: $clientSchemaPath"
    }
    if ((Get-RawSha256 -Path $schemaPath) -ne (Get-RawSha256 -Path $clientSchemaPath)) {
        throw "Server and client schema SHA256 values differ: '$schemaPath' versus '$clientSchemaPath'."
    }
}

& (Join-Path $PSScriptRoot 'Generate-Protocol.ps1') -Check
if ($LASTEXITCODE -ne 0) { throw 'Generated protocol drift check failed.' }

Write-Output "Schema SHA256=$(Get-RawSha256 -Path $schemaPath)"
Write-Output "Go output SHA256=$((Get-FileHash -Algorithm SHA256 -LiteralPath $generatedGoPath).Hash)"
