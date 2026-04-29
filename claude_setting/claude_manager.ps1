# 编码设置以支持中文
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
 
$ConfigDir = Join-Path $HOME ".claude"
$ConfigFile = Join-Path $ConfigDir "settings.json"

function Show-Welcome {
    # 定义颜色
    $orange = "$([char]27)[38;5;208m"
    $cyan = "$([char]27)[36m"
    $reset = "$([char]27)[0m"

    # 计算边框宽度
    $boxWidth = 79
    $line = "═" * ($boxWidth - 2)
    $line2 = " " * ($boxWidth - 2)

    Write-Host ""
    Write-Host "${orange}╔$line╗${reset}"
    Write-Host "${orange}║$line2║${reset}"
    Write-Host "${orange}║$line2║${reset}"
    Write-Host "${orange}║${reset}  ${orange}    ▐▛███▜▌${reset}      ${cyan}密钥设置-设置 Claude Code 认证的程序 V1.0.2 ${reset}              ${orange}║${reset}"
    Write-Host "${orange}║${reset}  ${orange}   ▝▜█████▛▘${reset}    ${cyan}你需要提前安装上 git、nodejs >= v22、Claude${reset}                ${orange}║${reset}"
    Write-Host "${orange}║${reset}  ${orange}     ▘▘ ▝▝${reset}    ${cyan}如果已安装，在你的项目文件夹下命令行执行 claude 进行开发    ${reset} ${orange}║${reset}"
    Write-Host "${orange}║$line2║${reset}"
    Write-Host "${orange}╚$line╝${reset}"
    Write-Host ""
}

function Check-Env {
    $nodeOk = $true
    $gitOk = $true
    $claudeOk = $true

    # 1. 检查 Node.js
    try {
        $nodeVer = node -v 2>$null
        if ($nodeVer) { Write-Host "[√] Node.js 已安装: $nodeVer" -ForegroundColor Green }
    } catch { 
        Write-Host "[X] 未检测到 Node.js" -ForegroundColor Red
        Write-Host "    - 安装建议: 访问 https://nodejs.org/ 下载安装 LTS 版本" -ForegroundColor Gray
        Write-Host "    - 或者 powershell Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))" -ForegroundColor Gray
        Write-Host "    - 然后安装 Node.js： choco install nodejs.install -y" -ForegroundColor Gray
       Write-Host "     - 设置国内加速源： npm config set registry https://registry.npmmirror.com/" -ForegroundColor Gray
        $nodeOk = $false 
    }


    # 2. 检查 Git
    try {
        $gitVer = git --version 2>$null
        if ($LASTEXITCODE -eq 0) { Write-Host "[√] Git 已安装: $gitVer" -ForegroundColor Green }
        else { throw }
    } catch {choco install git.install -y
        Write-Host "[X] 未检测到 Git" -ForegroundColor Red
        Write-Host "    - 安装建议: 访问 https://git-scm.com/ " -ForegroundColor Gray
        Write-Host "    - 或者 powershell Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))" -ForegroundColor Gray
        Write-Host "    - 然后安装 git： choco install git.install -y  最后执行git --version 检查是否安装成功" -ForegroundColor Gray
       $gitOk = $false
    }
    
    # 3. 检查 Claude Code
    try {
        Set-ExecutionPolicy RemoteSigned -Scope CurrentUser -Force
        $claudeVer = claude --version 2>$null
        if ($LASTEXITCODE -eq 0) { Write-Host "[√] Claude Code 已安装 :$claudeVer" -ForegroundColor Green }
        else { throw }
    } catch {
        Write-Host "[X] 未检测到 Claude Code" -ForegroundColor Red
        Write-Host "    - 安装建议(先确认nodejs安装了>=22版本): 请在终端执行 'npm install -g @anthropic-ai/claude-code --registry=https://registry.npmmirror.com'" -ForegroundColor Gray
        $claudeOk = $false
    }
    return ($nodeOk -and $gitOk -and $claudeOk)
}

function Set-ClaudeToken {
    # 先提示输入 ID
    $id = Read-Host "请输入您的 ID"
    
    # 验证 ID 输入
    if (-not $id) { 
        Write-Host "未输入 ID，取消操作。" -ForegroundColor Yellow
        return 
    }
    
    # 清理 ID（去除首尾空格）
    $id = $id.Trim()
    
    # 再提示输入 Token
    $token = Read-Host "请输入您的 TOKEN"
    
    # 验证 Token 输入
    if (-not $token) { 
        Write-Host "未输入 Token，取消操作。" -ForegroundColor Yellow
        return 
    }

    # 确保配置目录存在
    if (-not (Test-Path $ConfigDir)) { 
        New-Item -Path $ConfigDir -ItemType Directory | Out-Null 
    }

    # 构建 BASE_URL：地址 + ID
    $baseUrl = "http://dat6.com:18000/$id"

    # 创建配置对象
    $configObject = @{
        "env" = @{
            "ANTHROPIC_AUTH_TOKEN" = $token
            "ANTHROPIC_BASE_URL"   = $baseUrl
            "API_TIMEOUT_MS"       = "3000000"
            "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC" = "1"
        }
    }

    # 备份现有配置（如果存在）
    Restore-Config

    # 转换为 JSON 并保存
    try {
        $configObject | ConvertTo-Json -Depth 10 | Out-File -FilePath $ConfigFile -Encoding utf8
        Write-Host "配置已成功更新至 $ConfigFile" -ForegroundColor Green
        Write-Host "BASE_URL: $baseUrl" -ForegroundColor Cyan
    }
    catch {
        Write-Host "保存配置时出错: $_" -ForegroundColor Red
    }
}

function Restore-Config {
    if (Test-Path $ConfigFile) {
        $timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
        $backupFile = "$ConfigFile.bak_$timestamp"
        Rename-Item -Path $ConfigFile -NewName (Split-Path $backupFile -Leaf)
        Write-Host "原配置已备份为: $backupFile" -ForegroundColor Green
        Write-Host "配置文件已重置（已移除）。" -ForegroundColor Yellow
    } else {
        Write-Host "未发现配置文件，无需恢复。" -ForegroundColor Gray
    }
}

function Show-Menu {
    while ($true) {
        Write-Host "`n----------- 菜单 -----------" -ForegroundColor Cyan
        Write-Host "1. 设置 Token 并写入配置"
        Write-Host "2. 恢复配置 (备份并移除当前配置)"
        Write-Host "3. 启动 Claude Code"
        Write-Host "4. 退出"
        
        $choice = Read-Host "请选择 (1-4)"
        switch ($choice) {
            "1" { Set-ClaudeToken }
            "2" { Restore-Config }
            "3" { 
                Write-Host "正在启动 Claude Code..." -ForegroundColor Green
                claude 
            }
            "4" { exit }
            Default { Write-Host "无效选择，请重新输入" -ForegroundColor Red }
        }
    }
}

# 执行主流程
Clear-Host
Show-Welcome
if (Check-Env) {
    Show-Menu
} else {
    Write-Host "`n请先完成上述环境安装后再运行此脚本。" -ForegroundColor Magenta
    Write-Host "按任意键退出..."
    [void][System.Console]::ReadKey($true)
}