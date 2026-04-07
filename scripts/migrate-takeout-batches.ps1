<#
.SYNOPSIS
    Batch migration pipeline for Google Photos Takeout living on OneDrive.

.DESCRIPTION
    Iterates over each top-level subfolder of the Takeout source tree and
    drives a hydrate → fix → release cycle designed to keep the local disk
    from filling up when the source is stored in OneDrive "Files On-Demand"
    mode:

        1. Verify there is enough free space on C: for the next batch
           (2 x folder size + 5% margin). If not, pause and ask the user
           to confirm the previous upload batch finished, then clean
           `Processed/` to reclaim space.

        2. robocopy the batch from the OneDrive source to `Raw/` — this
           forces OneDrive to hydrate the placeholders into real files.

        3. Run GoogleTakeoutFixer on `Raw/{batch}` → `Staging/{batch}`
           to apply EXIF metadata.

        4. Delete `Raw/{batch}` and move `Staging/{batch}` → `Processed/{batch}`.
           The user then uploads `Processed/{batch}` to the new Google
           account via browser, and on the next run the completed folders
           can be cleaned to free disk space.

    State is persisted in `migration-progress.json` next to this script,
    so the pipeline is resumable after a crash or reboot.

    The source Takeout tree is treated as READ-ONLY at all times: the
    script only copies from it, never modifies or deletes.

.PARAMETER DryRun
    Simulate robocopy with /L and skip the actual fixer invocation. Use
    this to preview what the next batch would do without touching disk.

.PARAMETER SourceBase
    Root of the Google Photos Takeout export. Defaults to the OneDrive
    path used during the 2026 migration.

.PARAMETER ProjectRoot
    Path to the GoogleTakeoutFixer project (folder containing go.mod).
    Defaults to the local D:\ repo so the pipeline uses the patched
    version of the fixer, NOT the stale copy synced into OneDrive.

.PARAMETER FixerExe
    Path to a pre-built GoogleTakeoutFixer.exe. If provided, the script
    uses it directly and skips `go run`, which avoids recompilation
    overhead on every batch.

.EXAMPLE
    .\migrate-takeout-batches.ps1 -DryRun

.EXAMPLE
    .\migrate-takeout-batches.ps1 -FixerExe "D:\repos\GoogleTakeoutFixer_Windows_x64\GoogleTakeoutFixer\GoogleTakeoutFixer.exe"
#>
param(
    [Parameter(Mandatory=$false)]
    [switch]$DryRun = $false,

    [Parameter(Mandatory=$false)]
    [string]$SourceBase = "C:\Users\alexandre-machado\OneDrive\google takeout\Takeout\Google Photos",

    [Parameter(Mandatory=$false)]
    [string]$ProjectRoot = "D:\repos\GoogleTakeoutFixer_Windows_x64\GoogleTakeoutFixer",

    [Parameter(Mandatory=$false)]
    [string]$FixerExe = ""
)

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Definition
$tempBase = "C:\MigrationTemp"
$rawPath = Join-Path $tempBase "Raw"
$stagingPath = Join-Path $tempBase "Staging"
$processedPath = Join-Path $tempBase "Processed"
$stateFile = Join-Path $scriptPath "migration-progress.json"

# Begin transcript: captures all console output (including fixer stdout/stderr)
# to a timestamped file next to this script. Survives crashes and can be
# reviewed after an unattended run.
$transcriptFile = Join-Path $scriptPath ("migration-transcript-" + (Get-Date -Format "yyyy-MM-dd_HH-mm-ss") + ".log")
Start-Transcript -Path $transcriptFile -Append | Out-Null
Write-Host "Transcript: $transcriptFile" -ForegroundColor Gray

if ($DryRun) {
    Write-Host "!!! DRY RUN MODE ENABLED (SIMULATION) !!!" -ForegroundColor Black -BackgroundColor Yellow
}

# 1. Validate project root and resolve fixer invocation
if (!(Test-Path (Join-Path $ProjectRoot "go.mod"))) {
    Write-Host "[ERROR] Invalid ProjectRoot: '$ProjectRoot' does not contain go.mod." -ForegroundColor Red
    Write-Host "Pass -ProjectRoot pointing to the local repository root." -ForegroundColor Yellow
    exit 1
}

# Prefer a pre-built console-subsystem .exe (no recompilation per batch, stdout/stderr
# visible in the terminal). GoogleTakeoutFixerCLI.exe is built without -H=windowsgui so
# its output flows normally. The GUI exe (GoogleTakeoutFixer.exe) uses the Windows GUI
# subsystem and its output is swallowed when called from PowerShell — don't use it here.
# Fall back to `go run` if no binary is found.
$useExe = $false
if ($FixerExe -eq "") {
    $cliExe = Join-Path $ProjectRoot "GoogleTakeoutFixerCLI.exe"
    $guiExe = Join-Path $ProjectRoot "GoogleTakeoutFixer.exe"
    if (Test-Path $cliExe) {
        $FixerExe = $cliExe
    } elseif (Test-Path $guiExe) {
        Write-Host "WARNING: GoogleTakeoutFixerCLI.exe not found, using GUI binary." -ForegroundColor Yellow
        Write-Host "Fixer output may not be visible. Build the CLI binary with:" -ForegroundColor Yellow
        Write-Host "  CGO_ENABLED=1 go build -ldflags '-s -w' -o GoogleTakeoutFixerCLI.exe ./cmd/main.go" -ForegroundColor Yellow
        $FixerExe = $guiExe
    }
}
if ($FixerExe -ne "" -and (Test-Path $FixerExe)) {
    $useExe = $true
    Write-Host "Using pre-built binary: $FixerExe" -ForegroundColor Gray
} else {
    Write-Host "No .exe found - falling back to 'go run ./cmd' (slower)." -ForegroundColor Yellow
}

# 2. Load state and ensure folders exist
$progress = if (Test-Path $stateFile) { Get-Content $stateFile | ConvertFrom-Json } else { @{ completed = @(); completedWithWarnings = @() } }
# Ensure the completedWithWarnings field exists in older state files
if ($null -eq $progress.completedWithWarnings) { $progress | Add-Member -NotePropertyName completedWithWarnings -NotePropertyValue @() }
if (!(Test-Path $tempBase)) { New-Item -ItemType Directory -Path $tempBase | Out-Null }
if (!(Test-Path $rawPath)) { New-Item -ItemType Directory -Path $rawPath | Out-Null }
if (!(Test-Path $stagingPath)) { New-Item -ItemType Directory -Path $stagingPath | Out-Null }
if (!(Test-Path $processedPath)) { New-Item -ItemType Directory -Path $processedPath | Out-Null }

function Save-State { $script:progress | ConvertTo-Json -Depth 5 | Set-Content $stateFile }

function Mark-Completed($folder) {
    if ($folder -notin $script:progress.completed) {
        $script:progress.completed = @($script:progress.completed) + @($folder)
    }
    Save-State
}

function Mark-CompletedWithWarnings($folder) {
    if ($folder -notin $script:progress.completedWithWarnings) {
        $script:progress.completedWithWarnings = @($script:progress.completedWithWarnings) + @($folder)
    }
    Save-State
}

# Compute a folder's size WITHOUT hydrating its contents. Using the
# Scripting.FileSystemObject COM component reads the NTFS metadata
# directly, whereas Get-ChildItem -Recurse would trigger OneDrive
# downloads for placeholder files.
function Get-FolderSize ($path) {
    $size = 0
    $fso = New-Object -ComObject Scripting.FileSystemObject
    if ($fso.FolderExists($path)) {
        $folder = $fso.GetFolder($path)
        $size = $folder.Size
    }
    return $size
}

# 3. Configure ExifTool on PATH so the fixer's exec.LookPath finds it.
# We prefer the one bundled next to the fixer .exe (matches the release
# layout), otherwise assume it's already on PATH via scoop/winget.
if ($useExe) {
    $exifToolDir = Split-Path -Parent $FixerExe
    if (Test-Path (Join-Path $exifToolDir "exiftool.exe")) {
        $env:PATH = "$exifToolDir;$env:PATH"
        Write-Host "ExifTool detected next to the fixer and added to PATH." -ForegroundColor Gray
    }
}
if (!(Get-Command exiftool -ErrorAction SilentlyContinue)) {
    Write-Host "WARNING: exiftool.exe not found on PATH. Install via 'scoop install exiftool' or place it next to the .exe." -ForegroundColor Yellow
}

# 4. Main loop
$keepRunning = $true
$lastSelectedFolder = $null
$sameFolderRetries = 0
while ($keepRunning) {
    Write-Host "`n===============================================" -ForegroundColor Cyan
    Write-Host "   Google Photos Automatic Migration Queue     " -ForegroundColor Cyan
    Write-Host "===============================================" -ForegroundColor Cyan

    # Enumerate source folders using -LiteralPath-equivalent semantics. Trim any
    # trailing whitespace from the names and drop empty strings, so that invisible
    # filesystem artifacts (e.g. a "Z " with trailing space that came from an old
    # OneDrive sync) cannot diverge from what we wrote into the state file.
    $allFolders = @(
        Get-ChildItem -LiteralPath $SourceBase -Directory -ErrorAction SilentlyContinue |
            ForEach-Object { $_.Name.Trim() } |
            Where-Object { $_ -ne "" }
    )

    # Build a case-insensitive HashSet of "done" folders for reliable lookups
    # that don't depend on PowerShell's -notin behavior with PSCustomObject arrays.
    $doneSet = New-Object 'System.Collections.Generic.HashSet[string]' ([System.StringComparer]::OrdinalIgnoreCase)
    foreach ($n in @($progress.completed))              { if ($n) { [void]$doneSet.Add($n.Trim()) } }
    foreach ($n in @($progress.completedWithWarnings))  { if ($n) { [void]$doneSet.Add($n.Trim()) } }

    $pendingFolders = @($allFolders | Where-Object { -not $doneSet.Contains($_) })

    Write-Host ("Source folders: {0}  |  Done (clean+warn): {1}  |  Pending: {2}" -f `
        $allFolders.Count, $doneSet.Count, $pendingFolders.Count) -ForegroundColor Gray
    if ($pendingFolders.Count -gt 0) {
        $preview = $pendingFolders | Select-Object -First 5
        Write-Host ("Next up: {0}" -f ($preview -join " | ")) -ForegroundColor DarkGray
    }

    if ($pendingFolders.Count -eq 0) {
        Write-Host "Done! All folders have been processed." -ForegroundColor Green
        break
    }

    $selectedFolder = $pendingFolders[0]

    # Loop guard: if we keep picking the same folder without it moving to
    # completed, something is silently wrong (state not persisting, name
    # mismatch, etc.). Abort instead of spinning forever.
    if ($selectedFolder -eq $lastSelectedFolder) {
        $sameFolderRetries++
        if ($sameFolderRetries -ge 3) {
            Write-Host "`n[ABORT] Loop guard: folder '$selectedFolder' was selected $sameFolderRetries times in a row without leaving pending." -ForegroundColor Red
            Write-Host "        progress.completed count            : $(@($progress.completed).Count)" -ForegroundColor Yellow
            Write-Host "        progress.completedWithWarnings count : $(@($progress.completedWithWarnings).Count)" -ForegroundColor Yellow
            Write-Host "        Is folder in doneSet?                : $($doneSet.Contains($selectedFolder))" -ForegroundColor Yellow
            Write-Host "        State file: $stateFile" -ForegroundColor Yellow
            Write-Host "        Inspect the state file and folder name for hidden differences (trailing whitespace, Unicode lookalikes)." -ForegroundColor Yellow
            break
        }
    } else {
        $lastSelectedFolder = $selectedFolder
        $sameFolderRetries = 1
    }
    $sourcePath = Join-Path $SourceBase $selectedFolder
    $currentRaw = Join-Path $rawPath $selectedFolder
    $currentStaging = Join-Path $stagingPath $selectedFolder
    $currentProcessed = Join-Path $processedPath $selectedFolder

    # --- FREE-SPACE CHECK ---
    Write-Host "Analyzing next batch: '$selectedFolder'..." -ForegroundColor Gray

    $folderSizeBytes = Get-FolderSize $sourcePath
    $folderSizeGB = [math]::Round($folderSizeBytes / 1GB, 2)

    # 2x folder size (1x Raw + 1x Staging/Processed) + 5% safety margin
    $requiredSpaceBytes = ($folderSizeBytes * 2) * 1.05
    $requiredSpaceGB = [math]::Round($requiredSpaceBytes / 1GB, 2)

    $freeSpaceBytes = (Get-PSDrive C).Free
    $freeSpaceGB = [math]::Round($freeSpaceBytes / 1GB, 2)
    Write-Host "Pending batch(es)        : $($pendingFolders.Count)" -ForegroundColor Gray
    Write-Host "Estimated folder size    : $folderSizeGB GB" -ForegroundColor Gray
    Write-Host "Required space (w/ 5%)   : $requiredSpaceGB GB" -ForegroundColor Gray
    Write-Host "Current free space on C: : $freeSpaceGB GB" -ForegroundColor Gray

    if ($freeSpaceBytes -lt $requiredSpaceBytes) {
        Write-Host "`n[!] PAUSE: Insufficient space for the next batch." -ForegroundColor Yellow
        Write-Host "Batch '$selectedFolder' needs $requiredSpaceGB GB, but only $freeSpaceGB GB are free." -ForegroundColor Yellow
        Write-Host "The 'Processed' folder is full waiting for the browser upload to Google Photos to finish." -ForegroundColor Magenta

        $uploadDone = Read-Host "`nHas the browser upload for previous batches finished? (Y/N to clean and continue)"

        if ($uploadDone -eq "Y" -or $uploadDone -eq "y") {
            Write-Host "Clearing Processed folder to reclaim space..." -ForegroundColor Cyan
            Get-ChildItem -Path $processedPath -Directory | ForEach-Object {
                Remove-Item -Path $_.FullName -Recurse -Force
                Mark-Completed $_.Name
            }
            Write-Host "Cleanup done! Recomputing available space..." -ForegroundColor Green
            continue
        } else {
            Write-Host "Paused. Wait for the upload to finish and run the script again." -ForegroundColor Yellow
            break
        }
    }

    # Skip empty source folders (e.g. placeholder albums in the Takeout export).
    if ($folderSizeBytes -eq 0) {
        Write-Host "`n>>> SKIPPING: $selectedFolder (empty folder — nothing to copy)" -ForegroundColor Gray
        Mark-Completed $selectedFolder
        continue
    }

    Write-Host "`n>>> PROCESSING: $selectedFolder (space approved)" -ForegroundColor White -BackgroundColor Blue
    $batchHadWarnings = $false

    # --- STEP 1: COPY FROM ONEDRIVE (HYDRATE) ---
    # robocopy is the correct tool here: reading each file forces OneDrive to
    # download its content from the cloud (hydration). The risk is /MT with
    # too many threads overwhelming the OneDrive download queue, causing some
    # files to arrive as 0-byte stubs. Strategy:
    #   • First pass  : /MT:4 (parallel, faster)
    #   • If 0-bytes  : second pass /MT:1 (sequential, reliable) for those files
    #   • If still 0  : abort with clear message

    # Skip-copy only when existing Raw files all have real, clean content.
    # Triggers a fresh copy if: no files exist, any are 0-byte, or any still
    # carry NTFS cloud-sync attributes (OFFLINE 0x1000, RECALL_ON_DATA_ACCESS
    # 0x400000) — which means a previous robocopy run propagated OneDrive
    # placeholder attributes to the destination without stripping them.
    $cloudAttrMask = 0x1000 -bor 0x40000 -bor 0x400000
    $existingBatchFiles  = 0
    $existingZeroBytes   = 0
    $existingCloudStubs  = 0
    if (Test-Path -LiteralPath $currentRaw) {
        $existingRaw        = Get-ChildItem -LiteralPath $currentRaw -Recurse -File -ErrorAction SilentlyContinue
        $existingBatchFiles = $existingRaw.Count
        $existingZeroBytes  = ($existingRaw | Where-Object { $_.Length -eq 0 }).Count
        $existingCloudStubs = ($existingRaw | Where-Object { ([int]$_.Attributes -band $cloudAttrMask) -ne 0 }).Count
    }

    if ($existingBatchFiles -gt 0 -and $existingZeroBytes -eq 0 -and $existingCloudStubs -eq 0 -and !$DryRun) {
        Write-Host "`n[1/3] Raw\$selectedFolder already contains $existingBatchFiles clean file(s) (retry). Skipping copy." -ForegroundColor Yellow
    } else {
        if ($existingZeroBytes -gt 0 -or $existingCloudStubs -gt 0) {
            $reason = if ($existingCloudStubs -gt 0) { "$existingCloudStubs file(s) with cloud attributes (from old robocopy run)" } else { "$existingZeroBytes zero-byte file(s)" }
            Write-Host "`n[1/3] Raw\$selectedFolder has $reason. Re-copying with fixed flags..." -ForegroundColor Yellow
            Remove-Item -LiteralPath $currentRaw -Recurse -Force -ErrorAction SilentlyContinue
        } else {
            Write-Host "`n[1/3] Copying from OneDrive to local disk (Raw)..." -ForegroundColor Cyan
        }

        function Invoke-Robocopy {
            param($src, $dst, $threads, $isDryRun)
            $activity = "[1/3] Downloading '$selectedFolder' (threads: $threads)..."
            $job = Start-Job -ScriptBlock {
                param($s, $d, $t, $dry)
                $args = @($s, $d, "/E", "/R:3", "/W:5", "/MT:$t")
                if ($dry) { $args += "/L" }
                & robocopy @args
            } -ArgumentList $src, $dst, $threads, $isDryRun

            while ($job.State -eq "Running") {
                $bytes = 0
                if (Test-Path -LiteralPath $dst) {
                    $bytes = (Get-ChildItem -LiteralPath $dst -Recurse -File -ErrorAction SilentlyContinue |
                              Measure-Object -Property Length -Sum -ErrorAction SilentlyContinue).Sum
                    if ($null -eq $bytes) { $bytes = 0 }
                }
                $pct = if ($folderSizeBytes -gt 0) { [math]::Min(99, [int](($bytes / $folderSizeBytes) * 100)) } else { 0 }
                Write-Progress -Activity $activity -Status "$([math]::Round($bytes/1GB,2)) GB / $folderSizeGB GB" -PercentComplete $pct
                Start-Sleep -Seconds 2
            }
            Write-Progress -Activity $activity -Completed
            Receive-Job -Job $job -Wait | Out-Null
            Remove-Job -Job $job
        }

        # First pass: /MT:4
        Invoke-Robocopy -src $sourcePath -dst $currentRaw -threads 4 -isDryRun $DryRun

        if (!$DryRun) {
            $rawFiles  = Get-ChildItem -LiteralPath $currentRaw -Recurse -File -ErrorAction SilentlyContinue

            if ($rawFiles.Count -eq 0) {
                Write-Host "`n[ERROR] Copy produced no files in '$currentRaw'. Aborting." -ForegroundColor Red
                break
            }

            # Strip NTFS cloud-sync attributes that robocopy copied from the OneDrive
            # source (OFFLINE 0x1000, RECALL_ON_OPEN 0x40000, RECALL_ON_DATA_ACCESS
            # 0x400000). In a non-OneDrive folder these bits are meaningless but would
            # cause the fixer to wrongly treat a fully-downloaded file as a placeholder.
            # All other attributes (read-only, compressed, encrypted…) are preserved.
            #
            # [System.IO.FileAttributes] only covers the standard .NET subset of NTFS
            # attribute bits — RECALL_ON_OPEN (0x40000) and RECALL_ON_DATA_ACCESS
            # (0x400000) are not in that enum, so casting throws. Use the Win32
            # SetFileAttributes API directly via P/Invoke instead.
            if (-not ([System.Management.Automation.PSTypeName]'Win32FileAttr').Type) {
                Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public class Win32FileAttr {
    [DllImport("kernel32.dll", SetLastError=true, CharSet=CharSet.Unicode)]
    public static extern bool SetFileAttributes(string lpFileName, uint dwFileAttributes);
}
'@
            }
            $cloudAttrMask = 0x1000 -bor 0x40000 -bor 0x400000
            $stripped = 0
            $rawFiles | ForEach-Object {
                $raw = [uint32]$_.Attributes
                if ($raw -band $cloudAttrMask) {
                    $newAttrs = [uint32]($raw -band (-bnot $cloudAttrMask))
                    if ([Win32FileAttr]::SetFileAttributes($_.FullName, $newAttrs)) {
                        $stripped++
                    } else {
                        Write-Host "  [WARN] Could not clear cloud attributes on $($_.Name)" -ForegroundColor Yellow
                    }
                }
            }
            if ($stripped -gt 0) {
                Write-Host "  Stripped cloud-sync attributes from $stripped file(s)." -ForegroundColor Gray
                $rawFiles = Get-ChildItem -LiteralPath $currentRaw -Recurse -File -ErrorAction SilentlyContinue
            }

            $zeroFiles = $rawFiles | Where-Object { $_.Length -eq 0 }

            if ($zeroFiles.Count -gt 0) {
                Write-Host "  $($zeroFiles.Count) file(s) arrived as 0-byte reparse stubs — retrying with Copy-Item (forces content read)..." -ForegroundColor Yellow

                # robocopy with /MT can copy OneDrive reparse-point metadata
                # instead of content. Copy-Item follows reparse points and reads
                # actual bytes, guaranteeing hydration without admin rights.
                foreach ($stub in $zeroFiles) {
                    $relativePath = $stub.FullName.Substring($currentRaw.Length).TrimStart('\')
                    $srcFile = Join-Path $sourcePath $relativePath
                    try {
                        Copy-Item -LiteralPath $srcFile -Destination $stub.FullName -Force
                    } catch {
                        Write-Host "  [WARN] Could not copy $relativePath : $_" -ForegroundColor Yellow
                    }
                }

                $rawFiles  = Get-ChildItem -LiteralPath $currentRaw -Recurse -File -ErrorAction SilentlyContinue
                $zeroFiles = $rawFiles | Where-Object { $_.Length -eq 0 }

                if ($zeroFiles.Count -gt 0) {
                    Write-Host "`n[ERROR] $($zeroFiles.Count) file(s) still 0-byte after Copy-Item retry:" -ForegroundColor Red
                    $zeroFiles | ForEach-Object { Write-Host "  $($_.Name)" -ForegroundColor Red }
                    Write-Host "OneDrive may still be downloading. Wait a moment and re-run." -ForegroundColor Yellow
                    Remove-Item -LiteralPath $currentRaw -Recurse -Force -ErrorAction SilentlyContinue
                    break
                }
            }

            Write-Host "$($rawFiles.Count) file(s) copied and verified (all non-zero)." -ForegroundColor Gray
        }
    }

    # --- STEP 2: PROCESS (invoke the fixer) ---
    # Pass $rawPath (the parent) as --input so the fixer discovers $selectedFolder
    # as a subdirectory to process. Passing $currentRaw directly would make the
    # fixer treat the album folder as the root and find no subdirectories.
    # Since Raw is cleaned between batches, only the current batch folder is present.
    Write-Host "`n[2/3] Processing metadata (Raw\$selectedFolder -> Staging)..." -ForegroundColor Cyan
    $processedOk = $false

    if (!$DryRun) {
        $fixerArgs = @(
            "--input", $rawPath,
            "--output", $stagingPath,
            "--month-subfolders",
            "--restore-mov"
        )

        # Always run from $ProjectRoot so the fixer writes its logs/ folder
        # to a known, consistent location regardless of where this script
        # was launched from.
        # Pipe through Tee-Object so the fixer's stdout/stderr lands both on
        # the console AND in a per-batch log file — PowerShell 7's transcript
        # does not capture external-process output directly.
        $batchLogFile = Join-Path $ProjectRoot "logs" ("batch-$($selectedFolder -replace '[\\/:*?\[\]""<>|]','_')-$(Get-Date -Format 'yyyyMMdd_HHmmss').log")
        $null = New-Item -ItemType Directory -Path (Join-Path $ProjectRoot "logs") -Force

        $oldDir = Get-Location
        Set-Location $ProjectRoot
        try {
            if ($useExe) {
                & $FixerExe @fixerArgs 2>&1 | Tee-Object -FilePath $batchLogFile -Append
            } else {
                & go run ./cmd @fixerArgs 2>&1 | Tee-Object -FilePath $batchLogFile -Append
            }
        } finally {
            Set-Location $oldDir
        }

        Write-Host "  Fixer log: $batchLogFile" -ForegroundColor Gray
        Write-Host "  Detailed log: $ProjectRoot\logs\" -ForegroundColor Gray

        if ($LASTEXITCODE -eq 0) {
            $processedOk = $true
        } elseif ($LASTEXITCODE -eq 2) {
            # Exit code 2 = completed with warnings (no errors, but some files skipped)
            $processedOk = $true
            $batchHadWarnings = $true
            Write-Host "  Fixer completed with warnings (exit code 2). Review the log." -ForegroundColor Yellow
        } else {
            Write-Host "Fixer failed (exit code $LASTEXITCODE). See log above for details." -ForegroundColor Red
        }
    } else { $processedOk = $true }

    if (!$processedOk) { break }

    # --- STEP 3: RELEASE RAW + MOVE STAGING -> PROCESSED ---
    Write-Host "`n[3/3] Releasing Raw and preparing batch for upload..." -ForegroundColor Cyan
    if (!$DryRun) {
        if (Test-Path -LiteralPath $currentRaw) {
            # Brief wait in case the fixer process still holds file handles.
            Start-Sleep -Seconds 2
            try {
                Remove-Item -LiteralPath $currentRaw -Recurse -Force -ErrorAction Stop
            } catch {
                Write-Host "  [WARN] Could not delete Raw folder (file in use?): $_" -ForegroundColor Yellow
                Write-Host "  Raw\$selectedFolder will be cleaned up on the next run." -ForegroundColor Yellow
                $batchHadWarnings = $true
            }
        }

        if (Test-Path -LiteralPath $currentStaging) {
            # Disambiguate if Processed already has this batch name
            if (Test-Path -LiteralPath $currentProcessed) {
                $timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
                $currentProcessed = Join-Path $processedPath "$selectedFolder-$timestamp"
                Rename-Item -LiteralPath $currentStaging -NewName "$selectedFolder-$timestamp"
                $currentStaging = Join-Path $stagingPath "$selectedFolder-$timestamp"
            }

            Move-Item -LiteralPath $currentStaging -Destination $processedPath -Force
            Write-Host "Batch ready in Processed as: $(Split-Path $currentProcessed -Leaf)" -ForegroundColor Green
        } elseif ($processedOk) {
            # Fixer exited successfully but created no output folder — the batch
            # contained no media files (e.g. a Google Takeout "notas" folder).
            # Not an error: mark as completed and continue.
            Write-Host "No media files in '$selectedFolder' — skipped (nothing to upload)." -ForegroundColor Gray
        } else {
            Write-Host "`n[!] ERROR: Staging does not contain the expected folder '$selectedFolder'." -ForegroundColor Red
            Write-Host "GoogleTakeoutFixer may have produced a different layout. Current contents of Staging:" -ForegroundColor Yellow
            Get-ChildItem -LiteralPath $stagingPath -Directory -ErrorAction SilentlyContinue | ForEach-Object {
                Write-Host "  - $($_.Name)" -ForegroundColor Yellow
            }
            Write-Host "Fix manually and retry. The batch was NOT marked as completed." -ForegroundColor Red
            break
        }
    }

    # Mark as dispatched so it isn't picked up again on the next iteration.
    # Batches with non-fatal warnings go to completedWithWarnings for review;
    # clean batches go to completed.
    if ($batchHadWarnings) {
        Mark-CompletedWithWarnings $selectedFolder
        Write-Host "`nBatch '$selectedFolder' dispatched with warnings — review the log." -ForegroundColor Yellow
    } else {
        Mark-Completed $selectedFolder
        Write-Host "`nBatch '$selectedFolder' dispatched." -ForegroundColor Green
    }
    Start-Sleep -Seconds 2
}

Stop-Transcript | Out-Null
Write-Host "`nFull transcript saved to: $transcriptFile" -ForegroundColor Gray
