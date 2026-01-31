@echo off
echo Start building...

set OUTPUT_DIR=dist
set APP_NAME=aimanager

if exist %OUTPUT_DIR% rmdir /s /q %OUTPUT_DIR%
mkdir %OUTPUT_DIR%

echo Building Windows...
go build -o %OUTPUT_DIR%/%APP_NAME%-windows.exe -ldflags="-s -w" .

echo Building Linux...
SET CGO_ENABLED=0
SET GOOS=linux
SET GOARCH=amd64
go build -o %OUTPUT_DIR%/%APP_NAME%-linux -ldflags="-s -w" .

SET CGO_ENABLED=
SET GOOS=
SET GOARCH=

echo Done! Output in %OUTPUT_DIR% directory
