$ErrorActionPreference = 'Stop'
# Resource filenames include the target architecture so concurrent builds never
# race or include a resource compiled for the other Windows architecture.
$version = ($env:RELEASE_VERSION -replace '^v', '') -replace '-.*$', ''
$parts = $version.Split('.') | ForEach-Object { [int]$_ }
$metadata = @{
    FixedFileInfo = @{
        FileVersion = @{ Major = $parts[0]; Minor = $parts[1]; Patch = $parts[2]; Build = 0 }
        ProductVersion = @{ Major = $parts[0]; Minor = $parts[1]; Patch = $parts[2]; Build = 0 }
        FileType = '01'
    }
    StringFileInfo = @{
        CompanyName = 'AniList-Arr-Sync contributors'
        FileDescription = 'AniList-Arr-Sync'
        FileVersion = $env:RELEASE_VERSION
        ProductName = 'AniList-Arr-Sync'
        ProductVersion = $env:RELEASE_VERSION
        OriginalFilename = 'anilist-arr-sync.exe'
    }
    VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } }
}
$metadata | ConvertTo-Json -Depth 5 | Set-Content .release/versioninfo.json -Encoding utf8
foreach ($arch in 'amd64', 'arm64') {
    $flags = @('-64')
    if ($arch -eq 'arm64') { $flags += '-arm' }
    go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.5.0 @flags "-o=cmd/anilist-arr-sync/resource_windows_${arch}.syso" .release/versioninfo.json
    if ($LASTEXITCODE -ne 0) { throw "Windows resource generation failed: $arch" }
}
