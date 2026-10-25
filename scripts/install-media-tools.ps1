$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$toolsPath = Join-Path $projectRoot '.tools/ffmpeg'
New-Item -ItemType Directory -Path $toolsPath -Force | Out-Null
$archivePath = Join-Path $toolsPath 'ffmpeg-release-essentials.zip'
$checksumPath = Join-Path $toolsPath 'ffmpeg-release-essentials.zip.sha256'
curl.exe -fsSL --connect-timeout 20 --max-time 240 'https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip' -o $archivePath
if ($LASTEXITCODE -ne 0) { throw 'Media tools download failed' }
curl.exe -fsSL --connect-timeout 20 --max-time 30 'https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip.sha256' -o $checksumPath
if ($LASTEXITCODE -ne 0) { throw 'Media tools checksum download failed' }
$expectedHash=([IO.File]::ReadAllText($checksumPath).Trim() -split '\s+')[0]
if ((Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash -ne $expectedHash) { throw 'Media tools checksum mismatch' }
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive=[IO.Compression.ZipFile]::OpenRead($archivePath)
try {
 $probe=$archive.Entries | Where-Object { $_.FullName -match '/bin/ffprobe\.exe$' } | Select-Object -First 1
 if (!$probe) { throw 'ffprobe was not found in the archive' }
 [IO.Compression.ZipFileExtensions]::ExtractToFile($probe,(Join-Path $toolsPath 'ffprobe.exe'),$true)
 foreach($entry in $archive.Entries | Where-Object { $_.FullName -match '/(LICENSE|README)\.txt$' }) {
  [IO.Compression.ZipFileExtensions]::ExtractToFile($entry,(Join-Path $toolsPath ([IO.Path]::GetFileName($entry.FullName))),$true)
 }
} finally { $archive.Dispose() }
Write-Output 'ffprobe installed and SHA256 verified.'
