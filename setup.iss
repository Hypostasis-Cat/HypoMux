; ============================================================================
;  HypoMux 2.0 完全体最高规格标准安装脚本 (Inno Setup) - 最终完美修正版
; ============================================================================

#define MyAppName "HypoMux"
; 版本号优先由 CI 通过 iscc /D 注入；本地手动编译时回退到下方默认值。
; MyAppVersion 为显示版本（可为 v2.0.0 / dev-abc123 等任意字符串）
; MyAppVersionInfo 为写入 EXE 文件版本信息的纯数字版本（必须是 x.x.x[.x]）
#ifndef MyAppVersion
  #define MyAppVersion "2.0.0"
#endif
#ifndef MyAppVersionInfo
  #define MyAppVersionInfo "2.0.0"
#endif
#define MyAppPublisher "Hypostasis-Cat"
#define MyAppURL "https://github.com/Hypostasis-Cat/HypoMux"
#define MyAppExeName "HypoMux.exe"
#define MyAppRunValueName "HypoMux"

[Setup]
AppId={{7637d353-b9c0-4145-bc81-7a474e534d07}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}

; ---------- 系统管理员级标准安装（完美对齐公共根目录） ----------
PrivilegesRequired=admin
DefaultDirName={commonpf}\HypoMux
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes

; ---------- 现代扁平向导皮肤 ----------
WizardStyle=modern
Compression=lzma2
SolidCompression=yes
OutputDir=Output
OutputBaseFilename=HypoMux_Setup_{#MyAppVersion}
SetupIconFile=assets\icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
VersionInfoVersion={#MyAppVersionInfo}
; ---------- 安装包 EXE 版本资源（右键属性→详细信息） ----------
VersionInfoProductName={#MyAppName}
VersionInfoProductVersion={#MyAppVersionInfo}
VersionInfoCompany={#MyAppPublisher}
VersionInfoDescription={#MyAppName} Setup
VersionInfoCopyright=Copyright (C) 2026 {#MyAppPublisher}
ArchitecturesInstallIn64BitMode=x64compatible

[Languages]
; 简体中文语言文件随仓库携带，避免 GitHub Actions runner 缺失语言包导致打包失败
Name: "chinesesimplified"; MessagesFile: "Inno_Languages\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "autostart"; Description: "{cm:AutoStartTask}"; Flags: unchecked

[Files]
; 1. 全量打包 Nuitka standalone 模式输出的纯 C 二进制依赖矩阵文件夹
Source: "dist\main.dist\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

; 2. 核心修复：全量把 support 文件夹装箱打包，确保安装后 C:\Program Files\HypoMux\support 物理存在！
Source: "support\*"; DestDir: "{app}\support"; Flags: ignoreversion recursesubdirs createallsubdirs

; 3. 释放同级诊断内核与 bin 目录下网络接管三大运行时资产
Source: "diagnostic.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "bin\sing-box.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "bin\wintun.dll"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "bin\libcronet.dll"; DestDir: "{app}\bin"; Flags: ignoreversion

; 4. 图标资源：运行时窗口图标与系统托盘图标从 {app}\assets\icon.ico 读取
Source: "assets\icon.ico"; DestDir: "{app}\assets"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{commondesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
; ---------- 计划任务自启机制 ----------
Filename: "schtasks"; Parameters: "/Create /TN ""HypoMuxAutoStart"" /TR ""\""{app}\{#MyAppExeName}\"" --silent"" /SC ONLOGON /RL HIGHEST /F /RU INTERACTIVE"; \
    Flags: runhidden; Tasks: autostart

; ---------- 安装完成立刻运行机制 ----------
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#MyAppName}}"; \
    Flags: shellexec nowait postinstall skipifsilent

[UninstallRun]
; 卸载时干净利落地切断并销毁自启计划任务
Filename: "schtasks"; Parameters: "/Delete /TN ""HypoMuxAutoStart"" /F"; Flags: runhidden; RunOnceId: "DeleteHypoMuxTask"

[UninstallDelete]
Type: dirifempty; Name: "{app}"

[CustomMessages]
chinesesimplified.CreateDesktopIcon=创建桌面快捷方式
chinesesimplified.AdditionalIcons=附加快捷方式：
chinesesimplified.AutoStartTask=允许软件开机自动启动（以最高管理员权限静默驻留托盘）
chinesesimplified.LaunchProgram=立即运行 %1

english.CreateDesktopIcon=Create a desktop shortcut
english.AdditionalIcons=Additional shortcuts:
english.AutoStartTask=Launch HypoMux automatically at startup (Highest Admin Privileges - silent tray)
english.LaunchProgram=Run %1 now
