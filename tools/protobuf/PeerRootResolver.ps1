function Resolve-PeerRepositoryRoot {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)][string]$CurrentRoot,
        [string]$ExplicitPeerRoot,
        [Parameter(Mandatory = $true)][string]$PeerRepositoryName,
        [Parameter(Mandatory = $true)][string]$PeerDescription
    )

    $CurrentRoot = (Resolve-Path -LiteralPath $CurrentRoot).Path
    if (-not [string]::IsNullOrWhiteSpace($ExplicitPeerRoot)) {
        if (-not (Test-Path -LiteralPath $ExplicitPeerRoot -PathType Container)) {
            throw "Explicit $PeerDescription root does not exist: $ExplicitPeerRoot"
        }
        return (Resolve-Path -LiteralPath $ExplicitPeerRoot).Path
    }

    $currentParent = Split-Path $CurrentRoot -Parent
    if ((Split-Path $currentParent -Leaf) -ne '.worktrees') {
        throw "Omitted $PeerDescription root is allowed only from a coordination worktree; pass the peer root explicitly."
    }

    $workspaceRoot = Split-Path (Split-Path $currentParent -Parent) -Parent
    $worktreeName = Split-Path $CurrentRoot -Leaf
    $derivedRoot = Join-Path $workspaceRoot "$PeerRepositoryName\.worktrees\$worktreeName"
    if (-not (Test-Path -LiteralPath $derivedRoot -PathType Container)) {
        throw "Matching $PeerDescription coordination worktree does not exist: $derivedRoot"
    }
    return (Resolve-Path -LiteralPath $derivedRoot).Path
}
