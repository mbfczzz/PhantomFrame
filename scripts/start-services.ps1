$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $projectRoot
$toolsDir = Join-Path $projectRoot '.tools'
$mysqlBase = Join-Path $toolsDir 'mysql/mysql-8.4.13-winx64'
$redisBase = Join-Path $toolsDir 'redis/Redis-8.10.2-Windows-x64-msys2'
New-Item -ItemType Directory -Force $toolsDir | Out-Null
if (!(Test-Path -LiteralPath (Join-Path $mysqlBase 'bin/mysqld.exe'))) {
    curl.exe -fsSL --retry 2 'https://cdn.mysql.com/Downloads/MySQL-8.4/mysql-8.4.13-winx64.zip' -o .tools/mysql.zip
    if ($LASTEXITCODE -ne 0) { throw 'MySQL download failed' }
    if ((Get-FileHash .tools/mysql.zip -Algorithm MD5).Hash -ne '7B94489D5524DEA9B27097068BEA25A8') { throw 'MySQL checksum mismatch' }
    Expand-Archive -LiteralPath .tools/mysql.zip -DestinationPath .tools/mysql -Force
}
if (!(Test-Path -LiteralPath (Join-Path $redisBase 'redis-server.exe'))) {
    $release = Invoke-RestMethod 'https://api.github.com/repos/redis-windows/redis-windows/releases/tags/8.10.2'
    $asset = $release.assets | Where-Object { $_.name -eq 'Redis-8.10.2-Windows-x64-msys2.zip' }
    curl.exe -fsSL --retry 2 $asset.browser_download_url -o .tools/redis.zip
    if ($LASTEXITCODE -ne 0) { throw 'Redis download failed' }
    if (!$asset.digest -or ('sha256:' + (Get-FileHash .tools/redis.zip -Algorithm SHA256).Hash.ToLower()) -ne $asset.digest) { throw 'Redis checksum mismatch' }
    Expand-Archive -LiteralPath .tools/redis.zip -DestinationPath .tools/redis -Force
}
function New-LocalSecret {
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($bytes)
    $rng.Dispose()
    return ([BitConverter]::ToString($bytes)).Replace('-', '').ToLower()
}
$utf8 = New-Object System.Text.UTF8Encoding($false)
$mysqlData = Join-Path $projectRoot 'data/mysql'
$redisData = Join-Path $projectRoot 'data/redis'
$adminConfig = Join-Path $projectRoot 'data/mysql-admin.cnf'
$envPath = Join-Path $projectRoot '.env'
New-Item -ItemType Directory -Force (Join-Path $projectRoot 'data'), $redisData | Out-Null
if (!(Test-Path -LiteralPath $envPath)) {
    $sqlPassword = New-LocalSecret
    $redisPassword = New-LocalSecret
    $settings = @"
HOST=127.0.0.1
PORT=3001
APP_URL=http://127.0.0.1:5173
APP_ENV=development
DEMO_MODE=true
COOKIE_SECURE=false
DATA_DIR=./data
MYSQL_HOST=127.0.0.1
MYSQL_PORT=3307
MYSQL_USER=phantomframe
MYSQL_PASSWORD=$sqlPassword
MYSQL_DATABASE=phantomframe
REDIS_ADDR=127.0.0.1:6380
REDIS_PASSWORD=$redisPassword
REDIS_DB=0
REDIS_PREFIX=phantomframe:
"@
    [IO.File]::WriteAllText($envPath, $settings, $utf8)
}
$settingsMap = @{}
foreach ($line in [IO.File]::ReadAllLines($envPath)) {
    if ($line -match '^([A-Z_]+)=(.*)$') { $settingsMap[$matches[1]] = $matches[2] }
}
if ($settingsMap.MYSQL_HOST -ne '127.0.0.1' -or $settingsMap.MYSQL_PORT -ne '3307' -or $settingsMap.REDIS_ADDR -ne '127.0.0.1:6380') { throw 'This helper only manages the project local MySQL :3307 and Redis :6380. Use your configured external services directly.' }
$sqlPassword = $settingsMap.MYSQL_PASSWORD
$redisPassword = $settingsMap.REDIS_PASSWORD
if ($sqlPassword -notmatch '^[a-f0-9]{64}$' -or $redisPassword -notmatch '^[a-f0-9]{64}$') { throw 'Expected generated local service credentials in .env' }
$mysqld = Join-Path $mysqlBase 'bin/mysqld.exe'
$mysqlClient = Join-Path $mysqlBase 'bin/mysql.exe'
if (!(Test-Path -LiteralPath (Join-Path $mysqlData 'mysql'))) {
    New-Item -ItemType Directory -Force $mysqlData | Out-Null
    & $mysqld --initialize-insecure "--basedir=$mysqlBase" "--datadir=$mysqlData" --console
    if ($LASTEXITCODE -ne 0) { throw 'MySQL initialization failed' }
}
$sqlListener = Get-NetTCPConnection -LocalPort 3307 -State Listen -ErrorAction SilentlyContinue
if ($sqlListener) {
    if ((Get-Process -Id $sqlListener[0].OwningProcess).Path -ne $mysqld) { throw 'Port 3307 belongs to another process' }
} else {
    Start-Process -FilePath $mysqld -ArgumentList @("--basedir=$mysqlBase", "--datadir=$mysqlData", '--port=3307', '--bind-address=127.0.0.1', '--mysqlx=OFF', '--console') -WindowStyle Hidden -RedirectStandardOutput data/mysql.stdout.log -RedirectStandardError data/mysql.stderr.log | Out-Null
}
$ready = $false
for ($i=0; $i -lt 60; $i++) {
    try { $socket = New-Object Net.Sockets.TcpClient('127.0.0.1',3307); $socket.Close(); $ready=$true; break } catch { Start-Sleep -Milliseconds 500 }
}
if (!$ready) { throw 'MySQL did not start; inspect data/mysql.stderr.log' }
if (!(Test-Path -LiteralPath $adminConfig)) {
    $rootPassword = New-LocalSecret
    $sql = "CREATE DATABASE IF NOT EXISTS phantomframe CHARACTER SET utf8mb4 COLLATE utf8mb4_bin; CREATE USER IF NOT EXISTS 'phantomframe'@'127.0.0.1' IDENTIFIED BY '$sqlPassword'; GRANT ALL PRIVILEGES ON phantomframe.* TO 'phantomframe'@'127.0.0.1'; GRANT ALL PRIVILEGES ON ``phantomframe\_%``.* TO 'phantomframe'@'127.0.0.1'; ALTER USER 'root'@'localhost' IDENTIFIED BY '$rootPassword';"
    $sql | & $mysqlClient --protocol=TCP --host=127.0.0.1 --port=3307 --user=root
    if ($LASTEXITCODE -ne 0) { throw 'MySQL account initialization failed' }
    [IO.File]::WriteAllText($adminConfig, "[client]`nhost=127.0.0.1`nport=3307`nuser=root`npassword=$rootPassword`n", $utf8)
}
$redisExe = Join-Path $redisBase 'redis-server.exe'
$redisConf = Join-Path $projectRoot 'data/redis.conf'
$redisDirectory = '.'
[IO.File]::WriteAllText($redisConf, "bind 127.0.0.1`nport 6380`nprotected-mode yes`nrequirepass $redisPassword`nappendonly yes`nappendfsync everysec`ndir `"$redisDirectory`"`n", $utf8)
$redisListener = Get-NetTCPConnection -LocalPort 6380 -State Listen -ErrorAction SilentlyContinue
if ($redisListener) {
    if ((Get-Process -Id $redisListener[0].OwningProcess).Path -ne $redisExe) { throw 'Port 6380 belongs to another process' }
} else {
    Start-Process -FilePath $redisExe -ArgumentList @('../redis.conf') -WorkingDirectory $redisData -WindowStyle Hidden -RedirectStandardOutput (Join-Path $projectRoot 'data/redis.stdout.log') -RedirectStandardError (Join-Path $projectRoot 'data/redis.stderr.log') | Out-Null
}
$ready = $false
for ($i=0; $i -lt 30; $i++) {
    try { $socket = New-Object Net.Sockets.TcpClient('127.0.0.1',6380); $socket.Close(); $ready=$true; break } catch { Start-Sleep -Milliseconds 300 }
}
if (!$ready) { throw 'Redis did not start; inspect data/redis.stdout.log' }
Write-Output 'Project MySQL :3307 and Redis :6380 started. Credentials are stored in .env; services listen on localhost only.'
