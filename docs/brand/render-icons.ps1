# Screenshots docs/brand/icons.html (made by logo.mjs) with headless Chrome,
# crops each icon into static/, and builds static/favicon.ico from the 16,
# 32 and 48 px PNGs. Run after `node docs/brand/logo.mjs`.
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$static = Join-Path $here '..\..\static' | Resolve-Path
$chrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
$icons = Get-Content (Join-Path $here 'icons.json') -Raw | ConvertFrom-Json

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "brand-$PID"
New-Item -ItemType Directory -Force $tmp | Out-Null
$shot = Join-Path $tmp 'icons.png'
$width = ($icons | ForEach-Object { $_.x + $_.size } | Measure-Object -Maximum).Maximum + 8
$url = 'file:///' + ((Join-Path $here 'icons.html') -replace '\\', '/')
& $chrome --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 `
  --default-background-color=00000000 --allow-file-access-from-files "--user-data-dir=$tmp\prof" `
  "--window-size=$width,600" "--screenshot=$shot" $url 2>$null | Out-Null
if (-not (Test-Path $shot)) { throw 'Chrome made no screenshot' }

Add-Type -AssemblyName System.Drawing
$sheet = [System.Drawing.Bitmap]::new($shot)
foreach ($i in $icons) {
  $crop = $sheet.Clone([System.Drawing.Rectangle]::new($i.x, 0, $i.size, $i.size), [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
  $crop.Save((Join-Path $static $i.file), [System.Drawing.Imaging.ImageFormat]::Png)
  $crop.Dispose()
  "wrote static/$($i.file)"
}
$sheet.Dispose()

# favicon.ico: an ICONDIR header, one 16-byte entry per image, then the PNGs.
$pngs = 'favicon-16x16.png', 'favicon-32x32.png', 'favicon-48x48.png' | ForEach-Object { , [System.IO.File]::ReadAllBytes((Join-Path $static $_)) }
$sizes = 16, 32, 48
$ms = [System.IO.MemoryStream]::new()
$w = [System.IO.BinaryWriter]::new($ms)
$w.Write([uint16]0); $w.Write([uint16]1); $w.Write([uint16]$pngs.Count)
$offset = 6 + 16 * $pngs.Count
for ($k = 0; $k -lt $pngs.Count; $k++) {
  $w.Write([byte]$sizes[$k]); $w.Write([byte]$sizes[$k]); $w.Write([byte]0); $w.Write([byte]0)
  $w.Write([uint16]1); $w.Write([uint16]32); $w.Write([uint32]$pngs[$k].Length); $w.Write([uint32]$offset)
  $offset += $pngs[$k].Length
}
foreach ($p in $pngs) { $w.Write($p) }
$w.Flush()
[System.IO.File]::WriteAllBytes((Join-Path $static 'favicon.ico'), $ms.ToArray())
"wrote static/favicon.ico"
Remove-Item -Recurse -Force $tmp
