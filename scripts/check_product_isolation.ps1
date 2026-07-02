$ErrorActionPreference = 'Stop'

function Get-Deps {
    param([Parameter(Mandatory = $true)][string]$Package)
    $deps = go list -deps $Package
    if ($LASTEXITCODE -ne 0) { throw "go list failed for $Package" }
    return $deps
}

function Get-Imports {
    param([Parameter(Mandatory = $true)][string]$Package)
    $imports = go list -f '{{range .Imports}}{{println .}}{{end}}' $Package
    if ($LASTEXITCODE -ne 0) { throw "go list imports failed for $Package" }
    return $imports
}

function Assert-NoForbiddenDeps {
    param(
        [Parameter(Mandatory = $true)][string]$Product,
        [Parameter(Mandatory = $true)][string[]]$Deps,
        [Parameter(Mandatory = $true)][string[]]$Forbidden
    )
    $violations = @()
    foreach ($pattern in $Forbidden) {
        $matched = $Deps | Where-Object { $_ -eq $pattern -or $_.StartsWith($pattern + '/') }
        if ($matched) { $violations += $matched }
    }
    if ($violations.Count -gt 0) {
        $unique = $violations | Sort-Object -Unique
        throw "$Product contains forbidden dependencies:`n$($unique -join "`n")"
    }
}

function Assert-AllowedDirectImports {
    param(
        [Parameter(Mandatory = $true)][string]$Product,
        [Parameter(Mandatory = $true)][string[]]$Imports,
        [Parameter(Mandatory = $true)][string[]]$AllowedProjectImports
    )
    $projectImports = $Imports | Where-Object { $_.StartsWith('genesis-agent/') }
    $violations = @()
    foreach ($import in $projectImports) {
        if ($AllowedProjectImports -notcontains $import) { $violations += $import }
    }
    if ($violations.Count -gt 0) {
        $unique = $violations | Sort-Object -Unique
        throw "$Product entry imports unexpected project packages:`n$($unique -join "`n")"
    }
}

function Test-PackagePatternExists {
    param([Parameter(Mandatory = $true)][string]$Pattern)
    $packages = go list $Pattern 2>$null
    if ($LASTEXITCODE -ne 0) { return @() }
    return $packages
}

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    if ([string]::IsNullOrWhiteSpace($env:GOCACHE)) {
        $env:GOCACHE = Join-Path $repoRoot '.gocache'
    }
    New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null

    $internalPackages = go list ./internal/...
    if ($LASTEXITCODE -ne 0) { throw 'go list failed for ./internal/...' }
    foreach ($pkg in $internalPackages) {
        $deps = Get-Deps $pkg
        Assert-NoForbiddenDeps $pkg $deps @(
            'genesis-agent/products',
            'genesis-agent/shared/local'
        )
    }

    $sharedLocalPackages = Test-PackagePatternExists './shared/local/...'
    foreach ($pkg in $sharedLocalPackages) {
        $deps = Get-Deps $pkg
        Assert-NoForbiddenDeps $pkg $deps @(
            'genesis-agent/products',
            'github.com/wailsapp/wails',
            'github.com/jackc/pgx',
            'github.com/lib/pq'
        )
    }

    $cliImports = Get-Imports './cmd/genesis-cli'
    Assert-AllowedDirectImports 'CLI entry' $cliImports @('genesis-agent/products/cli/bootstrap')

    $desktopImports = Get-Imports './cmd/genesis-desktop'
    Assert-AllowedDirectImports 'Desktop entry' $desktopImports @('genesis-agent/products/desktop/bootstrap')

    $enterpriseImports = Get-Imports './cmd/genesis-enterprise'
    Assert-AllowedDirectImports 'Enterprise entry' $enterpriseImports @('genesis-agent/products/enterprise/bootstrap')

    $cliDeps = Get-Deps './cmd/genesis-cli'
    Assert-NoForbiddenDeps 'CLI' $cliDeps @(
        'genesis-agent/products/desktop',
        'genesis-agent/products/enterprise',
        'github.com/jackc/pgx',
        'github.com/lib/pq',
        'github.com/wailsapp/wails'
    )

    $desktopDeps = Get-Deps './cmd/genesis-desktop'
    Assert-NoForbiddenDeps 'Desktop' $desktopDeps @(
        'genesis-agent/products/cli',
        'genesis-agent/products/enterprise',
        'github.com/jackc/pgx',
        'github.com/lib/pq'
    )

    $enterpriseDeps = Get-Deps './cmd/genesis-enterprise'
    Assert-NoForbiddenDeps 'Enterprise' $enterpriseDeps @(
        'genesis-agent/products/cli',
        'genesis-agent/products/desktop',
        'genesis-agent/shared/local',
        'github.com/wailsapp/wails'
    )

    Write-Host 'Product isolation check passed.'
}
finally {
    Pop-Location
}
