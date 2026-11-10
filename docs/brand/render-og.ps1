# Screenshots docs/brand/og.html with headless Chrome into static/img/og.jpg,
# the 1200 x 630 link preview card (og:image in layouts/base.html).
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$out = Join-Path $here '..\..\static\img\og.jpg'
$chrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "og-$PID"
New-Item -ItemType Directory -Force $tmp | Out-Null
$shot = Join-Path $tmp 'og.png'
$url = 'file:///' + ((Join-Path $here 'og.html') -replace '\\', '/')
& $chrome --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 `
  --allow-file-access-from-files --virtual-time-budget=2000 "--user-data-dir=$tmp\prof" `
  '--window-size=1200,630' "--screenshot=$shot" $url 2>$null | Out-Null
if (-not (Test-Path $shot)) { throw 'Chrome made no screenshot' }

# JPEG at quality 90: WhatsApp drops preview images much over 300 KB, and
# the PNG is about 230 KB.
Add-Type -AssemblyName System.Drawing
$img = [System.Drawing.Image]::FromFile($shot)
$codec = [System.Drawing.Imaging.ImageCodecInfo]::GetImageEncoders() | Where-Object MimeType -eq 'image/jpeg'
$params = [System.Drawing.Imaging.EncoderParameters]::new(1)
$params.Param[0] = [System.Drawing.Imaging.EncoderParameter]::new([System.Drawing.Imaging.Encoder]::Quality, [long]90)
$img.Save([System.IO.Path]::GetFullPath($out), $codec, $params)
$img.Dispose()
Remove-Item -Recurse -Force $tmp
"wrote static/img/og.jpg"
