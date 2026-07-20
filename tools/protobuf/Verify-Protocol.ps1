[CmdletBinding()]
param([string]$ClientRoot)

$ErrorActionPreference = 'Stop'
$backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$schemaPath = Join-Path $backendRoot 'proto\game\v1\messages.proto'
$generatedGoPath = Join-Path $backendRoot 'internal\protocolpb\messages.pb.go'

function Assert-Contains {
    param([string]$Content, [string]$Expected)
    if ($Content -notmatch [regex]::Escape($Expected)) {
        throw "Expected protocol contract text is absent: $Expected"
    }
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
    'BattleOutcome outcome = 7;',
	'string run_id = 7;',
    'bytes args_json = 2;'
) | ForEach-Object { Assert-Contains -Content $schema -Expected $_ }

if (-not (Test-Path -LiteralPath $generatedGoPath)) {
    throw "Generated Go protocol is missing: $generatedGoPath"
}
$generated = Get-Content -LiteralPath $generatedGoPath -Raw
Assert-Contains -Content $generated -Expected 'protoc-gen-go v1.36.11'

& (Join-Path $PSScriptRoot 'Generate-Protocol.ps1') -ClientRoot $ClientRoot -Check
if ($LASTEXITCODE -ne 0) { throw 'Generated protocol drift check failed.' }

Write-Output "Schema SHA256=$((Get-FileHash -Algorithm SHA256 -LiteralPath $schemaPath).Hash)"
Write-Output "Go output SHA256=$((Get-FileHash -Algorithm SHA256 -LiteralPath $generatedGoPath).Hash)"
