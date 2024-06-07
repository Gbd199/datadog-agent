
$ErrorActionPreference = "Continue"
# $source="https://github.com/DataDog/datadog-ci/releases/download/v2.33.0/datadog-ci_win-x64"
# $folder = "c:\devtools\datadog-ci"
# $target = "c:\devtools\datadog-ci\datadog-ci"
# New-Item -ItemType Directory -Path $folder
# (New-Object System.Net.WebClient).DownloadFile($source, $target)
# $oldPath=[Environment]::GetEnvironmentVariable("Path", [System.EnvironmentVariableTarget]::User)
# $newPath="$oldPath;$folder"
# [Environment]::SetEnvironmentVariable("Path", $newPath, [System.EnvironmentVariableTarget]::User)
# $keypath = "HKLM:\SOFTWARE\DatadogDeveloper\datadog-ci"
# New-Item -Path $keypath -Force
# New-ItemProperty -Path $keypath -Name "version" -Value "2.33.0" -PropertyType String -Force
Invoke-WebRequest -Uri "https://github.com/DataDog/datadog-ci/releases/download/v2.37.0/datadog-ci_win-x64" -OutFile "datadog-ci.exe"
$DATADOG_API_KEY=$(& "$env:CI_PROJECT_DIR\tools\ci\aws_ssm_get_wrapper.ps1" $API_KEY_ORG2_SSM_NAME)
$env:DATADOG_API_KEY=$DATADOG_API_KEY
Get-ChildItem -Path "$CI_PROJECT_DIR" -Filter "junit-*.tgz" -Recurse | ForEach-Object { 
    tar -xzf $_.FullName
    Get-ChildItem -Path "$CI_PROJECT_DIR" -Filter "*junit*.xml" -Recurse | ForEach-Object {
        .\datadog-ci.exe junit upload --service "datadog-agent" $_.FullName
    }
    # inv -e junit-upload --tgz-path $_.FullName 
}