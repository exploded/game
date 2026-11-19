# Runs the cases from gen-cases.mjs through Rob's spreadsheets in Excel (over
# COM) and saves what Excel works out, for mortgage.test.mjs and
# super.test.mjs. The spreadsheets stay out of git (.local/calculators).
#
# PowerShell on James's PC, in C:\Projects\go\game\testdata\calculators\excel
# (needs Excel installed):
#
#   node gen-cases.mjs
#   .\excel-capture.ps1 -Mortgage "..\..\..\.local\calculators\CPW Mortgage Freedom Calculator Aug 26 (1).xlsx" `
#                       -Super "..\..\..\.local\calculators\Super Starting Early Calculator Aug 2026.xlsx"
#   cd ..\..\..; node --test "testdata/calculators/*.test.mjs"
#
# Each workbook is opened read-only and closed without saving.
param([string]$Mortgage, [string]$Super)

$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$out = Split-Path $here -Parent

# An Excel error (#N/A and the like) comes back as an Int32 code; save null.
function Get-Num($v) {
    if ($v -is [int]) { return $null }
    return [double]$v
}

$xl = New-Object -ComObject Excel.Application
$xl.Visible = $false; $xl.DisplayAlerts = $false
try {
    if ($Mortgage) {
        $cases = Get-Content "$here\mortgage-inputs.json" -Raw | ConvertFrom-Json
        $wb = $xl.Workbooks.Open((Resolve-Path $Mortgage).Path, 0, $true)
        $m = $wb.Worksheets.Item('Mortgage Freedom'); $s = $wb.Worksheets.Item('Calculation Schedule')
        try { $m.Unprotect() } catch {}
        $res = @()
        foreach ($c in $cases) {
            $m.Range('C7').Value2 = [double]$c.loan
            $m.Range('C8').Value2 = [double]$c.rate
            $m.Range('C9').Value2 = [double]$c.term
            $m.Range('C10').Value2 = [string]$c.freq
            $m.Range('F7').Value2 = [double]$c.extra
            $xl.CalculateFull()
            # Balances after these periods: F is the original schedule,
            # O the one with the extra repayment.
            $samples = @()
            foreach ($k in 1, 12, 26, 52, 100, 260, 520, 1000, 2600) {
                $samples += [pscustomobject]@{ period = $k; base = (Get-Num $s.Range("F$(1 + $k)").Value2); withExtra = (Get-Num $s.Range("O$(1 + $k)").Value2) }
            }
            $res += [pscustomobject]@{
                input        = $c
                regular      = Get-Num $m.Range('F8').Value2
                baseInterest = Get-Num $s.Range('Q3').Value2
                interest     = Get-Num $s.Range('Q4').Value2
                saved        = Get-Num $m.Range('B16').Value2
                periods      = Get-Num $s.Range('Q6').Value2
                monthsSaved  = Get-Num $s.Range('Q8').Value2
                samples      = $samples
            }
        }
        $wb.Close($false)
        $res | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8NoBOM "$out\mortgage-excel.json"
        "mortgage: $($res.Count) cases"
    }

    if ($Super) {
        $cases = Get-Content "$here\super-inputs.json" -Raw | ConvertFrom-Json
        $wb = $xl.Workbooks.Open((Resolve-Path $Super).Path, 0, $true)
        $sheet = $wb.Worksheets.Item('Calculations')
        # The Calculator sheet is password-protected, so the inputs go into
        # the hidden Calculations sheet's three input cells instead, worked
        # out exactly as its formulas do:
        #   G1 = Calculator!C6 * (1 - Calculator!F6)   weekly after contribution tax
        #   G2 = Calculator!C8 - Calculator!F8 * Calculator!I8   return after tax
        #   G4 = Calculator!I6                         inflation
        # Every figure after those comes from the sheet's own formulas.
        $res = @()
        foreach ($c in $cases) {
            $sheet.Range('G1').Value2 = [double]$c.weekly * (1 - [double]$c.ctax)
            $sheet.Range('G2').Value2 = [double]$c.ret - [double]$c.income * [double]$c.itax
            $sheet.Range('G4').Value2 = [double]$c.inflation
            $xl.CalculateFull()
            # Rows 4 to 44 are ages 20 to 60. H:J are future dollars for the
            # starts at 20, 30 and 40; B:D the same in today's dollars.
            $nominal = $sheet.Range('H4:J44').Value2; $today = $sheet.Range('B4:D44').Value2
            $paths = @(); $reals = @()
            foreach ($j in 1..3) {
                $paths += , @(1..41 | ForEach-Object { Get-Num $nominal[$_, $j] })
                $reals += , @(1..41 | ForEach-Object { Get-Num $today[$_, $j] })
            }
            $res += [pscustomobject]@{ input = $c; paths = $paths; reals = $reals }
        }
        $wb.Close($false)
        $res | ConvertTo-Json -Depth 6 -Compress | Set-Content -Encoding utf8NoBOM "$out\super-excel.json"
        "super: $($res.Count) cases"
    }
} finally {
    $xl.Quit()
    [void][System.Runtime.InteropServices.Marshal]::ReleaseComObject($xl)
}
