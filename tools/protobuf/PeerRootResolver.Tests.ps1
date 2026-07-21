$resolverPath = Join-Path $PSScriptRoot 'PeerRootResolver.ps1'

Describe 'Peer repository root resolution' {
    BeforeAll { . $resolverPath }

    BeforeEach {
        $fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ("peer roots with spaces-{0}" -f [guid]::NewGuid())
        $clientMain = Join-Path $fixtureRoot 'game-client-unity'
        $serverMain = Join-Path $fixtureRoot 'game-server-go'
        $clientWorktree = Join-Path $clientMain '.worktrees\sequence-test'
        $serverWorktree = Join-Path $serverMain '.worktrees\sequence-test'
        New-Item -ItemType Directory -Force -Path $clientMain, $serverMain, $clientWorktree, $serverWorktree | Out-Null
    }

    AfterEach { Remove-Item -LiteralPath $fixtureRoot -Recurse -Force -ErrorAction SilentlyContinue }

    It 'accepts an explicit peer path including spaces' {
        Resolve-PeerRepositoryRoot -CurrentRoot $serverMain -ExplicitPeerRoot $clientMain `
            -PeerRepositoryName 'game-client-unity' -PeerDescription 'client' | Should Be (Resolve-Path $clientMain).Path
    }

    It 'derives only the same-named coordination worktree' {
        Resolve-PeerRepositoryRoot -CurrentRoot $serverWorktree -PeerRepositoryName 'game-client-unity' `
            -PeerDescription 'client' | Should Be (Resolve-Path $clientWorktree).Path
    }

    It 'rejects a missing matching worktree instead of falling back to main' {
        Remove-Item -LiteralPath $clientWorktree -Recurse -Force
        { Resolve-PeerRepositoryRoot -CurrentRoot $serverWorktree -PeerRepositoryName 'game-client-unity' `
            -PeerDescription 'client' } | Should Throw
    }

    It 'requires an explicit peer outside a coordination worktree' {
        { Resolve-PeerRepositoryRoot -CurrentRoot $serverMain -PeerRepositoryName 'game-client-unity' `
            -PeerDescription 'client' } | Should Throw
    }
}
