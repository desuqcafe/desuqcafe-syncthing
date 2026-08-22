<#
.SYNOPSIS
    Draws the tray status icons and writes them as multi-size .ico files.

.DESCRIPTION
    The .ico files next to this script are committed, so building the tray does
    not depend on running this. Re-run it after changing a colour or a glyph:

        .\make-icons.ps1

    Icons are written as 32bpp DIB entries rather than embedded PNGs. systray
    on Windows writes the icon bytes to a temp file and hands the path to
    LoadImage(), and DIB entries are the format every Windows version loads
    without argument.

    At 16 pixels the glyph alone is not enough to tell states apart, so each
    state differs in both colour and shape.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
Add-Type -AssemblyName System.Drawing

# Tray icons are rendered at several sizes and Windows picks per DPI.
$Sizes = @(16, 20, 24, 32, 48)

$States = @(
    # name      disc colour   glyph
    @{ Name = 'idle';    Colour = '#8B5CF6'; Glyph = 'check' }
    @{ Name = 'syncing'; Colour = '#38BDF8'; Glyph = 'arrows' }
    @{ Name = 'paused';  Colour = '#64748B'; Glyph = 'pause' }
    @{ Name = 'error';   Colour = '#DC4B41'; Glyph = 'bang' }
    @{ Name = 'offline'; Colour = '#3F3F46'; Glyph = 'dash' }
)

function ConvertFrom-Hex {
    param([string]$Hex)
    [System.Drawing.Color]::FromArgb(
        255,
        [Convert]::ToInt32($Hex.Substring(1, 2), 16),
        [Convert]::ToInt32($Hex.Substring(3, 2), 16),
        [Convert]::ToInt32($Hex.Substring(5, 2), 16))
}

function New-StateBitmap {
    param([int]$Size, [System.Drawing.Color]$Colour, [string]$Glyph)

    # Draw at 4x and downsample: GDI+ antialiasing alone is not enough to keep
    # a 16 pixel glyph from looking ragged.
    $scale = 4
    $s = $Size * $scale
    $big = New-Object System.Drawing.Bitmap($s, $s, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($big)
    try {
        $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
        $g.Clear([System.Drawing.Color]::Transparent)

        $inset = $s * 0.02
        $disc = New-Object System.Drawing.RectangleF($inset, $inset, ($s - 2 * $inset), ($s - 2 * $inset))
        $brush = New-Object System.Drawing.SolidBrush($Colour)
        $g.FillEllipse($brush, $disc)
        $brush.Dispose()

        $white = [System.Drawing.Color]::FromArgb(255, 255, 255, 255)
        $pen = New-Object System.Drawing.Pen($white, [float]($s * 0.13))
        $pen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
        $pen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
        $pen.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
        $wb = New-Object System.Drawing.SolidBrush($white)

        switch ($Glyph) {
            'check' {
                $pts = @(
                    (New-Object System.Drawing.PointF([float]($s * 0.27), [float]($s * 0.52))),
                    (New-Object System.Drawing.PointF([float]($s * 0.43), [float]($s * 0.68))),
                    (New-Object System.Drawing.PointF([float]($s * 0.74), [float]($s * 0.34)))
                )
                $g.DrawLines($pen, [System.Drawing.PointF[]]$pts)
            }
            'arrows' {
                # A near-closed ring with an arrowhead: reads as "in motion"
                # at small sizes where two proper arrows would blur together.
                $r = $s * 0.24
                $arc = New-Object System.Drawing.RectangleF(
                    [float]($s / 2 - $r), [float]($s / 2 - $r), [float]($r * 2), [float]($r * 2))
                $pen.EndCap = [System.Drawing.Drawing2D.LineCap]::Flat
                $g.DrawArc($pen, $arc, 110, 285)
                $head = @(
                    (New-Object System.Drawing.PointF([float]($s * 0.50), [float]($s * 0.12))),
                    (New-Object System.Drawing.PointF([float]($s * 0.50), [float]($s * 0.40))),
                    (New-Object System.Drawing.PointF([float]($s * 0.26), [float]($s * 0.26)))
                )
                $g.FillPolygon($wb, [System.Drawing.PointF[]]$head)
            }
            'pause' {
                $bw = $s * 0.12
                $top = $s * 0.30
                $hgt = $s * 0.40
                $g.FillRectangle($wb, [float]($s * 0.33), [float]$top, [float]$bw, [float]$hgt)
                $g.FillRectangle($wb, [float]($s * 0.55), [float]$top, [float]$bw, [float]$hgt)
            }
            'bang' {
                $bw = $s * 0.13
                $g.FillRectangle($wb, [float]($s / 2 - $bw / 2), [float]($s * 0.24), [float]$bw, [float]($s * 0.34))
                $d = $s * 0.15
                $g.FillEllipse($wb, [float]($s / 2 - $d / 2), [float]($s * 0.64), [float]$d, [float]$d)
            }
            'dash' {
                $g.FillRectangle($wb, [float]($s * 0.28), [float]($s * 0.44), [float]($s * 0.44), [float]($s * 0.12))
            }
        }

        $pen.Dispose()
        $wb.Dispose()

        $out = New-Object System.Drawing.Bitmap($Size, $Size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $og = [System.Drawing.Graphics]::FromImage($out)
        try {
            $og.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $og.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
            $og.Clear([System.Drawing.Color]::Transparent)
            $og.DrawImage($big, (New-Object System.Drawing.Rectangle(0, 0, $Size, $Size)))
        } finally {
            $og.Dispose()
        }
        return $out
    } finally {
        $g.Dispose()
        $big.Dispose()
    }
}

# Returns the DIB payload for one icon directory entry: a BITMAPINFOHEADER with
# doubled height, then bottom-up BGRA pixels, then an all-zero AND mask (the
# alpha channel does the masking on 32bpp icons, but the mask must still be
# present and correctly sized).
function ConvertTo-IconDib {
    param([System.Drawing.Bitmap]$Bitmap)

    $w = $Bitmap.Width
    $h = $Bitmap.Height

    $rect = New-Object System.Drawing.Rectangle(0, 0, $w, $h)
    $data = $Bitmap.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly,
                             [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    try {
        $stride = $data.Stride
        $raw = New-Object byte[] ($stride * $h)
        [System.Runtime.InteropServices.Marshal]::Copy($data.Scan0, $raw, 0, $raw.Length)
    } finally {
        $Bitmap.UnlockBits($data)
    }

    $ms = New-Object System.IO.MemoryStream
    $bw = New-Object System.IO.BinaryWriter($ms)
    try {
        $bw.Write([uint32]40)      # biSize
        $bw.Write([int32]$w)       # biWidth
        $bw.Write([int32]($h * 2)) # biHeight: XOR image plus AND mask
        $bw.Write([uint16]1)       # biPlanes
        $bw.Write([uint16]32)      # biBitCount
        $bw.Write([uint32]0)       # biCompression = BI_RGB
        $bw.Write([uint32]0)       # biSizeImage
        $bw.Write([int32]0); $bw.Write([int32]0)
        $bw.Write([uint32]0); $bw.Write([uint32]0)

        # DIBs are stored bottom-up.
        for ($y = $h - 1; $y -ge 0; $y--) {
            $bw.Write($raw, $y * $stride, $w * 4)
        }

        $maskStride = [math]::Floor(($w + 31) / 32) * 4
        $bw.Write((New-Object byte[] ($maskStride * $h)), 0, $maskStride * $h)

        $bw.Flush()
        return $ms.ToArray()
    } finally {
        $bw.Dispose()
        $ms.Dispose()
    }
}

function Write-IcoFile {
    param([string]$Path, [byte[][]]$Images, [int[]]$Widths)

    $fs = [System.IO.File]::Create($Path)
    $bw = New-Object System.IO.BinaryWriter($fs)
    try {
        $bw.Write([uint16]0)                 # reserved
        $bw.Write([uint16]1)                 # type: icon
        $bw.Write([uint16]$Images.Count)

        # Image data starts after the directory.
        $offset = 6 + 16 * $Images.Count
        for ($i = 0; $i -lt $Images.Count; $i++) {
            # A 256 pixel entry is recorded as 0 in the directory; nothing here
            # is that large, but the rule is cheap to honour.
            $wpx = $Widths[$i]
            $dim = [byte]$(if ($wpx -ge 256) { 0 } else { $wpx })
            $bw.Write($dim)
            $bw.Write($dim)
            $bw.Write([byte]0)               # palette entries
            $bw.Write([byte]0)               # reserved
            $bw.Write([uint16]1)             # planes
            $bw.Write([uint16]32)            # bits per pixel
            $bw.Write([uint32]$Images[$i].Length)
            $bw.Write([uint32]$offset)
            $offset += $Images[$i].Length
        }
        foreach ($img in $Images) { $bw.Write($img, 0, $img.Length) }
        $bw.Flush()
    } finally {
        $bw.Dispose()
        $fs.Dispose()
    }
}

foreach ($state in $States) {
    $colour = ConvertFrom-Hex $state.Colour
    $images = @()
    foreach ($size in $Sizes) {
        $bmp = New-StateBitmap -Size $size -Colour $colour -Glyph $state.Glyph
        try {
            $images += , (ConvertTo-IconDib -Bitmap $bmp)
        } finally {
            $bmp.Dispose()
        }
    }
    $path = Join-Path $PSScriptRoot "$($state.Name).ico"
    Write-IcoFile -Path $path -Images $images -Widths $Sizes
    Write-Host ("{0,-9} {1}  {2:N1} KB" -f $state.Name, $state.Colour, ((Get-Item $path).Length / 1KB))
}
