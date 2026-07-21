[CmdletBinding()]
param([string]$ClientRoot)

$ErrorActionPreference = 'Stop'
$backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$schemaPath = Join-Path $backendRoot 'proto\game.proto'
$generatedGoPath = Join-Path $backendRoot 'internal\protocolpb\game.pb.go'
$codecPath = Join-Path $backendRoot 'internal\protocol\codec.go'
$kernelPath = Join-Path $backendRoot 'internal\kernel\kernel.go'
$connectionPath = Join-Path $backendRoot 'internal\transport\connection.go'
. (Join-Path $PSScriptRoot 'PeerRootResolver.ps1')

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
    'double survival_time = 5;',
    'BattleOutcome outcome = 7;',
    'string run_id = 7;',
    'bytes args_json = 2;'
) | ForEach-Object { Assert-Contains -Content $schema -Expected $_ }

if (-not (Test-Path -LiteralPath $generatedGoPath -PathType Leaf)) {
    throw "Generated Go protocol is missing: $generatedGoPath"
}
$generated = Get-Content -LiteralPath $generatedGoPath -Raw
Assert-Contains -Content $generated -Expected 'protoc-gen-go v1.36.11'

$codec = Get-Content -LiteralPath $codecPath -Raw
@(
    'HeaderSize   = 10',
    'binary.LittleEndian.PutUint32(frame[:4], uint32(totalLen))',
    'binary.LittleEndian.PutUint16(frame[4:6], msgID)',
    'binary.LittleEndian.PutUint32(frame[6:10], seq)',
    'Seq:   binary.LittleEndian.Uint32(data[6:10])'
) | ForEach-Object { Assert-Contains -Content $codec -Expected $_ }

$kernel = Get-Content -LiteralPath $kernelPath -Raw
@(
    'if frame.Seq == 0 {',
    'k.finish(sess, frame.Seq, entry, response, handlerErr)',
    'sess.Reply(seq, entry.respID, message)',
    'sess.Reply(seq, protocol.MsgID_Error'
) | ForEach-Object { Assert-Contains -Content $kernel -Expected $_ }

$connection = Get-Content -LiteralPath $connectionPath -Raw
Assert-Contains -Content $connection -Expected 'return c.sendMessage(0, msgID, payload)'

$ClientRoot = Resolve-PeerRepositoryRoot -CurrentRoot $backendRoot -ExplicitPeerRoot $ClientRoot `
    -PeerRepositoryName 'game-client-unity' -PeerDescription 'client'
$clientSchemaPath = Join-Path $ClientRoot 'proto\game.proto'
if (-not (Test-Path -LiteralPath $clientSchemaPath -PathType Leaf)) {
    throw "Sibling client schema is missing: $clientSchemaPath"
}
if ((Get-RawSha256 -Path $schemaPath) -ne (Get-RawSha256 -Path $clientSchemaPath)) {
    throw "Server and client schema SHA256 values differ: '$schemaPath' versus '$clientSchemaPath'."
}

& (Join-Path $PSScriptRoot 'Generate-Protocol.ps1') -Check
if ($LASTEXITCODE -ne 0) { throw 'Generated protocol drift check failed.' }

Push-Location $backendRoot
try {
    & go test ./internal/protocol ./internal/kernel ./internal/transport `
        -run 'Test(LoginReqGoldenSequencedFrame|DecodeRoundTripsSeqAndRejectsSixByteFrame|KernelEchoesRequestSeq|MalformedBodyErrorEchoesRequestSeq|KernelRejectsZeroSeqWithoutErrorFrame|HubPushFramesUseZeroSeq|ReadPumpClosesOnFatalProtocolFramesWithoutErrorResponse)$' `
        -count=1
    if ($LASTEXITCODE -ne 0) { throw 'Executable sequenced frame verification failed.' }
}
finally { Pop-Location }

Write-Output "Schema SHA256=$(Get-RawSha256 -Path $schemaPath)"
Write-Output "Go output SHA256=$((Get-FileHash -Algorithm SHA256 -LiteralPath $generatedGoPath).Hash)"
Write-Output 'FRAME_TESTS=PASS'
Write-Output 'FRAME_EVIDENCE=header=10 little_endian=1 request_seq_nonzero=1 response_seq_echo=1 pushes_seq_zero=1'
