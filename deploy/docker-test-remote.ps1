[CmdletBinding()]
param(
    [string]$SshTarget = $env:LINAI_TEST_SSH_TARGET,
    [int]$SshPort = 2222,
    [switch]$Stop
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($SshTarget)) {
    throw 'Set LINAI_TEST_SSH_TARGET to your SSH alias or user@host before running this script.'
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$sshPath = (Get-Command ssh.exe -ErrorAction Stop).Source
$scpPath = (Get-Command scp.exe -ErrorAction Stop).Source
$tarPath = (Get-Command tar.exe -ErrorAction Stop).Source
$remoteRoot = '.cache/linai-api-remote-test-worktree'
$sshOptions = @('-p', "$SshPort", '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', '-o', 'StrictHostKeyChecking=accept-new')

if ($Stop) {
    $stopCommand = "bash $remoteRoot/deploy/docker-test-remote.sh down"
    & $sshPath @sshOptions $SshTarget $stopCommand
    if ($LASTEXITCODE -ne 0) {
        throw "Remote stop failed with exit code $LASTEXITCODE."
    }
    return
}

$manifestPath = Join-Path $env:TEMP ("linai-docker-test-{0}.list" -f [guid]::NewGuid().ToString('N'))
$archivePath = Join-Path $env:TEMP ("linai-docker-test-{0}.tar.gz" -f [guid]::NewGuid().ToString('N'))

try {
    $gitPath = (Get-Command git.exe -ErrorAction Stop).Source
    $gitInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $gitInfo.FileName = $gitPath
    $gitInfo.Arguments = "-c `"safe.directory=$repoRoot`" -C `"$repoRoot`" ls-files --cached --others --exclude-standard -z"
    $gitInfo.UseShellExecute = $false
    $gitInfo.RedirectStandardOutput = $true
    $gitInfo.CreateNoWindow = $true

    $gitProcess = [System.Diagnostics.Process]::new()
    $gitProcess.StartInfo = $gitInfo
    if (-not $gitProcess.Start()) {
        throw 'Could not start git to collect the current working tree.'
    }
    $manifestStream = [System.IO.File]::Open($manifestPath, [System.IO.FileMode]::CreateNew)
    try {
        $gitProcess.StandardOutput.BaseStream.CopyTo($manifestStream)
    }
    finally {
        $manifestStream.Dispose()
    }
    $gitProcess.WaitForExit()
    if ($gitProcess.ExitCode -ne 0) {
        throw "git ls-files failed with exit code $($gitProcess.ExitCode)."
    }

    $gitEntries = [System.Text.Encoding]::UTF8.GetString([System.IO.File]::ReadAllBytes($manifestPath))
    $existingFiles = foreach ($relativePath in $gitEntries.Split([char]0)) {
        if ([string]::IsNullOrWhiteSpace($relativePath)) { continue }
        if ($relativePath.Contains("`n") -or $relativePath.Contains("`r")) {
            throw 'A source path contains a newline and cannot be safely packaged.'
        }
        if (($relativePath -match '(^|/)\.env($|\.)' -and $relativePath -notmatch '(^|/)\.env\.example$') -or
            $relativePath -match '(^|/)config(\.local)?\.ya?ml$' -or
            $relativePath -match '(^|/)plugins(/|$)' -or
            $relativePath -match '(^|/)(node_modules|vendor|\.git)(/|$)') {
            continue
        }
        $fullPath = Join-Path $repoRoot ($relativePath.Replace('/', [System.IO.Path]::DirectorySeparatorChar))
        if (Test-Path -LiteralPath $fullPath -PathType Leaf) {
            $relativePath
        }
    }
    if (-not $existingFiles) {
        throw 'No source files were found to upload.'
    }

    $manifestBytes = [System.Text.UTF8Encoding]::new($false).GetBytes(($existingFiles -join "`n") + "`n")
    [System.IO.File]::WriteAllBytes($manifestPath, $manifestBytes)

    & $tarPath -czf $archivePath -C $repoRoot -T $manifestPath
    if ($LASTEXITCODE -ne 0) {
        throw "Could not package the current working tree (tar exit code $LASTEXITCODE)."
    }

    $makeRemoteDirectory = "mkdir -p $remoteRoot"
    & $sshPath @sshOptions $SshTarget $makeRemoteDirectory
    if ($LASTEXITCODE -ne 0) {
        throw "Could not create the remote staging directory (SSH exit code $LASTEXITCODE)."
    }

    $remoteDirectory = "$SshTarget`:$remoteRoot/context.tar.gz"
    $scpOptions = @('-P', "$SshPort", '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15')
    & $scpPath @scpOptions $archivePath $remoteDirectory
    if ($LASTEXITCODE -ne 0) {
        throw "Source upload failed with exit code $LASTEXITCODE."
    }

    $remoteCommand = "tar -xzf $remoteRoot/context.tar.gz -C $remoteRoot --no-same-owner --overwrite && rm -f $remoteRoot/context.tar.gz && bash $remoteRoot/deploy/docker-test-remote.sh up"
    & $sshPath @sshOptions $SshTarget $remoteCommand
    if ($LASTEXITCODE -ne 0) {
        throw "Remote Docker build or health check failed with exit code $LASTEXITCODE."
    }
}
finally {
    Remove-Item -LiteralPath $manifestPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $archivePath -Force -ErrorAction SilentlyContinue
}
