; Tisane Windows Installer (NSIS 3.x Unicode)
;
; Supports:
;   - Dual install scope: per-user (%LOCALAPPDATA%\Programs\Tisane) and
;     per-machine (%ProgramFiles%\Tisane)
;   - On-demand UAC elevation (no UAC prompt for per-user installs; prompts
;     for UAC only when installing/uninstalling globally)
;   - Fresh install and in-place upgrades preserving user config and custom
;     dictionaries
;   - Optional Start Menu folder with shortcut to Tisane UI (checked by default)
;   - Optional PATH registration for Tisane CLI tools (unchecked by default)
;   - Windows Add/Remove Programs (Apps & Features) registration and uninstaller

Unicode True
SetCompressor /SOLID lzma
RequestExecutionLevel user
ManifestDPIAware true

!ifndef APP_VERSION
  !define APP_VERSION "0.0.0"
!endif
!ifndef APP_VERSION_QUAD
  !define APP_VERSION_QUAD "0.0.0.0"
!endif
!ifndef ICON_PATH
  !error "ICON_PATH must be defined"
!endif
!ifndef OUT_FILE
  !error "OUT_FILE must be defined"
!endif
!ifndef STAGING_DIR
  !error "STAGING_DIR must be defined"
!endif
!ifndef FILES_INSTALL_NSH
  !error "FILES_INSTALL_NSH must be defined"
!endif
!ifndef FILES_UNINSTALL_NSH
  !error "FILES_UNINSTALL_NSH must be defined"
!endif

!define APP_NAME "Tisane"
!define APP_PUBLISHER "Brian Beck"
!define APP_URL "https://github.com/beckbria/tisane"
!define APP_EXE "tisane.exe"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Tisane"

!include "MUI2.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"

!insertmacro GetParameters
!insertmacro GetOptions
!insertmacro GetSize
!insertmacro un.GetParameters
!insertmacro un.GetOptions

Name "${APP_NAME} ${APP_VERSION}"
OutFile "${OUT_FILE}"
BrandingText "${APP_NAME} ${APP_VERSION}"

VIProductVersion "${APP_VERSION_QUAD}"
VIAddVersionKey "ProductName" "${APP_NAME}"
VIAddVersionKey "CompanyName" "${APP_PUBLISHER}"
VIAddVersionKey "FileDescription" "${APP_NAME} Setup"
VIAddVersionKey "FileVersion" "${APP_VERSION}"
VIAddVersionKey "ProductVersion" "${APP_VERSION}"

; ---------------------------------------------------------------------------
; Modern UI Configuration
; ---------------------------------------------------------------------------

!define MUI_ICON "${ICON_PATH}"
!define MUI_UNICON "${ICON_PATH}"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "Welcome to ${APP_NAME} ${APP_VERSION} Setup"
!define MUI_WELCOMEPAGE_TEXT "This wizard will guide you through installing or upgrading ${APP_NAME} on your computer.$\r$\n$\r$\nClick Next to continue."

!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${APP_NAME}"
!define MUI_FINISHPAGE_RUN_FUNCTION "LaunchTisane"

; ---------------------------------------------------------------------------
; Installer Variables
; ---------------------------------------------------------------------------

Var InstallMode       ; "user" or "all"
Var IsAdmin           ; 0 or 1
Var SkipScopePage     ; 1 when relaunched with /ALLUSERS or /CURRENTUSER
Var DirFromCmdLine    ; 1 when /D=... was supplied on command line
Var ExistingUserDir   ; Existing per-user InstallLocation (if any)
Var ExistingAllDir    ; Existing global InstallLocation (if any)
Var CreateStartMenu   ; 1 (default) or 0
Var AddToPath         ; 0 (default) or 1
Var PrevPathAdded     ; Existing PathAdded DWORD before upgrade (0 or 1)
Var ExtraRelaunchArgs ; Preserved CLI args across UAC relaunch

Var Dialog
Var RadioUser
Var RadioAll
Var CheckStartMenu
Var CheckPath

; ---------------------------------------------------------------------------
; Installer Pages
; ---------------------------------------------------------------------------

!insertmacro MUI_PAGE_WELCOME
Page custom ScopePageCreate ScopePageLeave
!insertmacro MUI_PAGE_DIRECTORY
Page custom OptionsPageCreate OptionsPageLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

; Uninstaller Pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; ---------------------------------------------------------------------------
; Helper Functions
; ---------------------------------------------------------------------------

Function SetDefaultInstDir
  ${If} $DirFromCmdLine == 1
    Return
  ${EndIf}

  ${If} $InstallMode == "all"
    ${If} $ExistingAllDir != ""
      StrCpy $INSTDIR $ExistingAllDir
    ${Else}
      StrCpy $INSTDIR "$PROGRAMFILES64\${APP_NAME}"
    ${EndIf}
  ${Else}
    ${If} $ExistingUserDir != ""
      StrCpy $INSTDIR $ExistingUserDir
    ${Else}
      StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_NAME}"
    ${EndIf}
  ${EndIf}
FunctionEnd

Function LoadScopePreferences
  ; Load previous Start Menu / PATH choices when upgrading an existing install.
  ; Defaults: CreateStartMenu = 1, AddToPath = 0.
  StrCpy $CreateStartMenu 1
  StrCpy $AddToPath 0
  StrCpy $PrevPathAdded 0

  ${If} $InstallMode == "all"
    ClearErrors
    ReadRegDWORD $0 HKLM "${UNINST_KEY}" "StartMenuCreated"
    ${IfNot} ${Errors}
      StrCpy $CreateStartMenu $0
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKLM "${UNINST_KEY}" "PathAdded"
    ${IfNot} ${Errors}
      StrCpy $AddToPath $0
      StrCpy $PrevPathAdded $0
    ${EndIf}
  ${Else}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINST_KEY}" "StartMenuCreated"
    ${IfNot} ${Errors}
      StrCpy $CreateStartMenu $0
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINST_KEY}" "PathAdded"
    ${IfNot} ${Errors}
      StrCpy $AddToPath $0
      StrCpy $PrevPathAdded $0
    ${EndIf}
  ${EndIf}
FunctionEnd

Function LaunchTisane
  ; Launching via explorer.exe ensures Tisane runs at medium integrity
  ; (unelevated) even after an elevated all-users installation.
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\${APP_EXE}"'
FunctionEnd

Function AddInstallDirToPath
  ${If} $InstallMode == "all"
    StrCpy $0 "Machine"
  ${Else}
    StrCpy $0 "User"
  ${EndIf}

  System::Call 'Kernel32::SetEnvironmentVariable(t "TISANE_INSTALL_DIR", t "$INSTDIR")'
  System::Call 'Kernel32::SetEnvironmentVariable(t "TISANE_PATH_SCOPE", t "$0")'
  nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "$$dir = $$env:TISANE_INSTALL_DIR.TrimEnd(\"\\\"); $$scope = $$env:TISANE_PATH_SCOPE; $$cur = [Environment]::GetEnvironmentVariable(\"Path\", $$scope); $$parts = @(); if ($$cur) { $$parts = $$cur -split \";\" | Where-Object { $$_ -ne \"\" } }; if (-not ($$parts | Where-Object { $$_.TrimEnd(\"\\\") -ieq $$dir })) { $$parts += $$dir; [Environment]::SetEnvironmentVariable(\"Path\", ($$parts -join \";\"), $$scope) }"'
  Pop $1
  Pop $2
FunctionEnd

Function RemoveInstallDirFromPath
  ${If} $InstallMode == "all"
    StrCpy $0 "Machine"
  ${Else}
    StrCpy $0 "User"
  ${EndIf}

  System::Call 'Kernel32::SetEnvironmentVariable(t "TISANE_INSTALL_DIR", t "$INSTDIR")'
  System::Call 'Kernel32::SetEnvironmentVariable(t "TISANE_PATH_SCOPE", t "$0")'
  nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "$$dir = $$env:TISANE_INSTALL_DIR.TrimEnd(\"\\\"); $$scope = $$env:TISANE_PATH_SCOPE; $$cur = [Environment]::GetEnvironmentVariable(\"Path\", $$scope); if ($$cur) { $$parts = $$cur -split \";\" | Where-Object { $$_ -ne \"\" -and $$_.TrimEnd(\"\\\") -ine $$dir }; [Environment]::SetEnvironmentVariable(\"Path\", ($$parts -join \";\"), $$scope) }"'
  Pop $1
  Pop $2
FunctionEnd

Function un.RemoveInstallDirFromPath
  ; Expects $0 = "Machine" or "User"
  System::Call 'Kernel32::SetEnvironmentVariable(t "TISANE_INSTALL_DIR", t "$INSTDIR")'
  System::Call 'Kernel32::SetEnvironmentVariable(t "TISANE_PATH_SCOPE", t "$0")'
  nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "$$dir = $$env:TISANE_INSTALL_DIR.TrimEnd(\"\\\"); $$scope = $$env:TISANE_PATH_SCOPE; $$cur = [Environment]::GetEnvironmentVariable(\"Path\", $$scope); if ($$cur) { $$parts = $$cur -split \";\" | Where-Object { $$_ -ne \"\" -and $$_.TrimEnd(\"\\\") -ine $$dir }; [Environment]::SetEnvironmentVariable(\"Path\", ($$parts -join \";\"), $$scope) }"'
  Pop $1
  Pop $2
FunctionEnd

; ---------------------------------------------------------------------------
; Initialization (.onInit)
; ---------------------------------------------------------------------------

Function .onInit
  ${If} ${RunningX64}
    SetRegView 64
  ${Else}
    MessageBox MB_OK|MB_ICONSTOP "${APP_NAME} requires 64-bit Windows."
    Abort
  ${EndIf}

  ; Determine whether we already hold Administrator privileges.
  UserInfo::GetAccountType
  Pop $0
  ${If} $0 == "Admin"
    StrCpy $IsAdmin 1
  ${Else}
    StrCpy $IsAdmin 0
  ${EndIf}

  ; Detect if /D=... was passed on the command line (NSIS populates $INSTDIR early).
  ${If} $INSTDIR != ""
    StrCpy $DirFromCmdLine 1
  ${Else}
    StrCpy $DirFromCmdLine 0
  ${EndIf}

  ; Detect existing installations in HKCU and HKLM.
  ReadRegStr $ExistingUserDir HKCU "${UNINST_KEY}" "InstallLocation"
  ReadRegStr $ExistingAllDir  HKLM "${UNINST_KEY}" "InstallLocation"

  ${GetParameters} $R0
  StrCpy $SkipScopePage 0
  StrCpy $ExtraRelaunchArgs ""

  ClearErrors
  ${GetOptions} $R0 "/NOSTARTMENU" $R1
  ${IfNot} ${Errors}
    StrCpy $ExtraRelaunchArgs "$ExtraRelaunchArgs /NOSTARTMENU"
  ${EndIf}

  ClearErrors
  ${GetOptions} $R0 "/ADDTOPATH" $R1
  ${IfNot} ${Errors}
    StrCpy $ExtraRelaunchArgs "$ExtraRelaunchArgs /ADDTOPATH"
  ${EndIf}

  ClearErrors
  ${GetOptions} $R0 "/ALLUSERS" $R1
  ${IfNot} ${Errors}
    StrCpy $InstallMode "all"
    StrCpy $SkipScopePage 1
    ${If} $IsAdmin != 1
      StrCpy $R2 "/ALLUSERS$ExtraRelaunchArgs"
      ${If} ${Silent}
        StrCpy $R2 "/S $R2"
      ${EndIf}
      ${If} $DirFromCmdLine == 1
        StrCpy $R2 "$R2 /D=$INSTDIR"
      ${EndIf}
      ClearErrors
      ExecShell "runas" "$EXEPATH" "$R2"
      ${If} ${Errors}
        MessageBox MB_OK|MB_ICONEXCLAMATION "Administrator privileges are required to install ${APP_NAME} for all users."
        Abort
      ${EndIf}
      Quit
    ${EndIf}
  ${Else}
    ClearErrors
    ${GetOptions} $R0 "/CURRENTUSER" $R1
    ${IfNot} ${Errors}
      StrCpy $InstallMode "user"
      StrCpy $SkipScopePage 1
    ${Else}
      ; Default to global if an existing global install is found (and no user install).
      ${If} $ExistingAllDir != ""
      ${AndIf} $ExistingUserDir == ""
        StrCpy $InstallMode "all"
      ${Else}
        StrCpy $InstallMode "user"
      ${EndIf}
    ${EndIf}
  ${EndIf}

  Call SetDefaultInstDir
  Call LoadScopePreferences

  ; Apply explicit CLI overrides after loading saved preferences.
  ClearErrors
  ${GetOptions} $R0 "/NOSTARTMENU" $R1
  ${IfNot} ${Errors}
    StrCpy $CreateStartMenu 0
  ${EndIf}

  ClearErrors
  ${GetOptions} $R0 "/ADDTOPATH" $R1
  ${IfNot} ${Errors}
    StrCpy $AddToPath 1
  ${EndIf}
FunctionEnd

; ---------------------------------------------------------------------------
; Custom Page 1: Install Scope (Per-User vs Global with On-Demand UAC)
; ---------------------------------------------------------------------------

Function ScopePageCreate
  ${If} $SkipScopePage == 1
    Abort
  ${EndIf}

  !insertmacro MUI_HEADER_TEXT "Choose Install Scope" "Select whether to install ${APP_NAME} just for yourself or for all users."

  nsDialogs::Create 1018
  Pop $Dialog
  ${If} $Dialog == error
    Abort
  ${EndIf}

  ${NSD_CreateLabel} 0 0 100% 24u "Choose who should have access to ${APP_NAME} on this computer:"
  Pop $0

  ; Per-user option
  ${If} $ExistingUserDir != ""
    StrCpy $1 "Install just for me (upgrade existing installation at $ExistingUserDir)"
  ${Else}
    StrCpy $1 "Install just for me (recommended, no administrator privileges required)"
  ${EndIf}
  ${NSD_CreateRadioButton} 10u 28u 95% 18u $1
  Pop $RadioUser
  ${NSD_CreateLabel} 22u 46u 90% 14u "Default folder: $LOCALAPPDATA\Programs\${APP_NAME}"
  Pop $0

  ; Per-machine (global) option
  ${If} $ExistingAllDir != ""
    StrCpy $2 "Install for all users (upgrade existing installation at $ExistingAllDir)"
  ${Else}
    StrCpy $2 "Install for all users on this computer (requires administrator privileges / UAC)"
  ${EndIf}
  ${NSD_CreateRadioButton} 10u 70u 95% 18u $2
  Pop $RadioAll
  ${NSD_CreateLabel} 22u 88u 90% 14u "Default folder: $PROGRAMFILES64\${APP_NAME}"
  Pop $0

  ${If} $InstallMode == "all"
    ${NSD_SetState} $RadioAll ${BST_CHECKED}
  ${Else}
    ${NSD_SetState} $RadioUser ${BST_CHECKED}
  ${EndIf}

  nsDialogs::Show
FunctionEnd

Function ScopePageLeave
  ${NSD_GetState} $RadioAll $0
  ${If} $0 == ${BST_CHECKED}
    StrCpy $InstallMode "all"
    ${If} $IsAdmin != 1
      StrCpy $R2 "/ALLUSERS$ExtraRelaunchArgs"
      ${If} $DirFromCmdLine == 1
        StrCpy $R2 "$R2 /D=$INSTDIR"
      ${EndIf}
      ClearErrors
      ExecShell "runas" "$EXEPATH" "$R2"
      ${If} ${Errors}
        MessageBox MB_OK|MB_ICONEXCLAMATION "Administrator approval is required to install for all users. You can retry or select 'Install just for me'."
        Abort
      ${EndIf}
      Quit
    ${EndIf}
  ${Else}
    StrCpy $InstallMode "user"
  ${EndIf}

  Call SetDefaultInstDir
  Call LoadScopePreferences
FunctionEnd

; ---------------------------------------------------------------------------
; Custom Page 2: Installation Options (Start Menu & Optional PATH)
; ---------------------------------------------------------------------------

Function OptionsPageCreate
  !insertmacro MUI_HEADER_TEXT "Installation Options" "Choose optional shortcuts and command-line integration for ${APP_NAME}."

  nsDialogs::Create 1018
  Pop $Dialog
  ${If} $Dialog == error
    Abort
  ${EndIf}

  ${NSD_CreateLabel} 0 0 100% 18u "Select any additional tasks you would like Setup to perform:"
  Pop $0

  ${NSD_CreateCheckbox} 10u 26u 95% 14u "Create a Start Menu folder with a shortcut to ${APP_NAME}"
  Pop $CheckStartMenu
  ${If} $CreateStartMenu == 1
    ${NSD_SetState} $CheckStartMenu ${BST_CHECKED}
  ${Else}
    ${NSD_SetState} $CheckStartMenu ${BST_UNCHECKED}
  ${EndIf}
  ${NSD_CreateLabel} 22u 42u 90% 16u "Adds a '${APP_NAME}' folder to the Start Menu with shortcuts to the ${APP_NAME} UI and Uninstaller."
  Pop $0

  ${NSD_CreateCheckbox} 10u 68u 95% 14u "Add ${APP_NAME} CLI tools to PATH"
  Pop $CheckPath
  ${If} $AddToPath == 1
    ${NSD_SetState} $CheckPath ${BST_CHECKED}
  ${Else}
    ${NSD_SetState} $CheckPath ${BST_UNCHECKED}
  ${EndIf}
  ${NSD_CreateLabel} 22u 84u 90% 24u "Adds the installation directory to your PATH so tisane-cli, tisane-build, tisane-extract, and tisane-merge can be run from any terminal."
  Pop $0

  nsDialogs::Show
FunctionEnd

Function OptionsPageLeave
  ${NSD_GetState} $CheckStartMenu $0
  ${If} $0 == ${BST_CHECKED}
    StrCpy $CreateStartMenu 1
  ${Else}
    StrCpy $CreateStartMenu 0
  ${EndIf}

  ${NSD_GetState} $CheckPath $0
  ${If} $0 == ${BST_CHECKED}
    StrCpy $AddToPath 1
  ${Else}
    StrCpy $AddToPath 0
  ${EndIf}
FunctionEnd

; ---------------------------------------------------------------------------
; Main Install Section
; ---------------------------------------------------------------------------

Section "Install"
  ${If} $InstallMode == "all"
    SetShellVarContext all
  ${Else}
    SetShellVarContext current
  ${EndIf}

  ; Check whether tisane.exe is currently running/locked during an upgrade.
  CheckRunning:
  IfFileExists "$INSTDIR\${APP_EXE}" 0 InstallFiles
    ClearErrors
    FileOpen $0 "$INSTDIR\${APP_EXE}" a
    ${If} ${Errors}
      MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "${APP_NAME} appears to be running.$\r$\n$\r$\nPlease close ${APP_NAME} and click Retry to continue." /SD IDCANCEL IDRETRY CheckRunning
      Abort
    ${Else}
      FileClose $0
    ${EndIf}

  InstallFiles:
  SetOutPath "$INSTDIR"
  !include "${FILES_INSTALL_NSH}"

  SetOutPath "$INSTDIR"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Start Menu folder and shortcuts (opt-in, checked by default)
  ${If} $CreateStartMenu == 1
    CreateDirectory "$SMPROGRAMS\${APP_NAME}"
    CreateShortcut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\${APP_EXE}" 0
    CreateShortcut "$SMPROGRAMS\${APP_NAME}\Uninstall ${APP_NAME}.lnk" "$INSTDIR\uninstall.exe"
  ${Else}
    Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
    Delete "$SMPROGRAMS\${APP_NAME}\Uninstall ${APP_NAME}.lnk"
    RMDir  "$SMPROGRAMS\${APP_NAME}"
  ${EndIf}

  ; Optional PATH update (opt-in, unchecked by default)
  ${If} $AddToPath == 1
    Call AddInstallDirToPath
  ${ElseIf} $PrevPathAdded == 1
    Call RemoveInstallDirFromPath
  ${EndIf}

  ; Compute total installed size in KB for Add/Remove Programs
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0

  ; Register in Add/Remove Programs (Apps & Features)
  ${If} $InstallMode == "all"
    WriteRegStr   HKLM "${UNINST_KEY}" "DisplayName"          "${APP_NAME}"
    WriteRegStr   HKLM "${UNINST_KEY}" "DisplayVersion"       "${APP_VERSION}"
    WriteRegStr   HKLM "${UNINST_KEY}" "Publisher"            "${APP_PUBLISHER}"
    WriteRegStr   HKLM "${UNINST_KEY}" "URLInfoAbout"         "${APP_URL}"
    WriteRegStr   HKLM "${UNINST_KEY}" "DisplayIcon"          "$INSTDIR\${APP_EXE},0"
    WriteRegStr   HKLM "${UNINST_KEY}" "InstallLocation"      "$INSTDIR"
    WriteRegStr   HKLM "${UNINST_KEY}" "UninstallString"      '"$INSTDIR\uninstall.exe"'
    WriteRegStr   HKLM "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
    WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify"             1
    WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair"             1
    WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize"        "$0"
    WriteRegStr   HKLM "${UNINST_KEY}" "InstallMode"          "all"
    WriteRegDWORD HKLM "${UNINST_KEY}" "StartMenuCreated"     $CreateStartMenu
    WriteRegDWORD HKLM "${UNINST_KEY}" "PathAdded"            $AddToPath
  ${Else}
    WriteRegStr   HKCU "${UNINST_KEY}" "DisplayName"          "${APP_NAME}"
    WriteRegStr   HKCU "${UNINST_KEY}" "DisplayVersion"       "${APP_VERSION}"
    WriteRegStr   HKCU "${UNINST_KEY}" "Publisher"            "${APP_PUBLISHER}"
    WriteRegStr   HKCU "${UNINST_KEY}" "URLInfoAbout"         "${APP_URL}"
    WriteRegStr   HKCU "${UNINST_KEY}" "DisplayIcon"          "$INSTDIR\${APP_EXE},0"
    WriteRegStr   HKCU "${UNINST_KEY}" "InstallLocation"      "$INSTDIR"
    WriteRegStr   HKCU "${UNINST_KEY}" "UninstallString"      '"$INSTDIR\uninstall.exe"'
    WriteRegStr   HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify"             1
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair"             1
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize"        "$0"
    WriteRegStr   HKCU "${UNINST_KEY}" "InstallMode"          "user"
    WriteRegDWORD HKCU "${UNINST_KEY}" "StartMenuCreated"     $CreateStartMenu
    WriteRegDWORD HKCU "${UNINST_KEY}" "PathAdded"            $AddToPath
  ${EndIf}
SectionEnd

; ---------------------------------------------------------------------------
; Uninstaller
; ---------------------------------------------------------------------------

Var UnInstallMode
Var UnPathAdded

Function un.onInit
  ${If} ${RunningX64}
    SetRegView 64
  ${EndIf}

  ; Determine whether this uninstaller belongs to a global (HKLM) or per-user (HKCU) install.
  StrCpy $UnInstallMode "user"
  ReadRegStr $0 HKLM "${UNINST_KEY}" "InstallLocation"
  ${If} $0 == $INSTDIR
    StrCpy $UnInstallMode "all"
  ${Else}
    ReadRegStr $1 HKCU "${UNINST_KEY}" "InstallLocation"
    ${If} $1 != $INSTDIR
    ${AndIf} $0 != ""
      StrCpy $UnInstallMode "all"
    ${EndIf}
  ${EndIf}

  ; If uninstalling a global install without admin privileges, copy uninstaller to $TEMP
  ; before elevating so _?=$INSTDIR does not lock $INSTDIR\uninstall.exe during removal.
  ${If} $UnInstallMode == "all"
    UserInfo::GetAccountType
    Pop $0
    ${If} $0 != "Admin"
      ${un.GetParameters} $R0
      ClearErrors
      ${un.GetOptions} $R0 "_?=" $R1
      ${If} ${Errors}
        StrCpy $R0 '$R0 _?=$INSTDIR'
      ${EndIf}
      CopyFiles /SILENT "$INSTDIR\uninstall.exe" "$TEMP\tisane-uninstall.exe"
      ClearErrors
      ExecShell "runas" "$TEMP\tisane-uninstall.exe" "$R0"
      ${If} ${Errors}
        MessageBox MB_OK|MB_ICONEXCLAMATION "Administrator privileges are required to uninstall ${APP_NAME} for all users." /SD IDOK
        Abort
      ${EndIf}
      Quit
    ${EndIf}
  ${EndIf}
FunctionEnd

Section "Uninstall"
  ${If} $UnInstallMode == "all"
    SetShellVarContext all
    ReadRegDWORD $UnPathAdded HKLM "${UNINST_KEY}" "PathAdded"
  ${Else}
    SetShellVarContext current
    ReadRegDWORD $UnPathAdded HKCU "${UNINST_KEY}" "PathAdded"
  ${EndIf}

  ; Ensure tisane.exe is not running before removing files.
  UnCheckRunning:
  IfFileExists "$INSTDIR\${APP_EXE}" 0 UnRemoveFiles
    ClearErrors
    FileOpen $0 "$INSTDIR\${APP_EXE}" a
    ${If} ${Errors}
      MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "${APP_NAME} appears to be running.$\r$\n$\r$\nPlease close ${APP_NAME} and click Retry to continue." /SD IDCANCEL IDRETRY UnCheckRunning
      Abort
    ${Else}
      FileClose $0
    ${EndIf}

  UnRemoveFiles:
  !include "${FILES_UNINSTALL_NSH}"
  Delete "$INSTDIR\uninstall.exe"

  ; Remove Start Menu shortcuts & folder (if present)
  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}\Uninstall ${APP_NAME}.lnk"
  RMDir  "$SMPROGRAMS\${APP_NAME}"

  ; Remove from PATH only if the installer previously added it
  ${If} $UnPathAdded == 1
    ${If} $UnInstallMode == "all"
      StrCpy $0 "Machine"
    ${Else}
      StrCpy $0 "User"
    ${EndIf}
    Call un.RemoveInstallDirFromPath
  ${EndIf}

  ; Remove install directory only if empty (preserves any user-added dictionaries)
  RMDir "$INSTDIR\dictionaries"
  RMDir "$INSTDIR"

  ; Remove Add/Remove Programs registration
  ${If} $UnInstallMode == "all"
    DeleteRegKey HKLM "${UNINST_KEY}"
  ${Else}
    DeleteRegKey HKCU "${UNINST_KEY}"
  ${EndIf}
SectionEnd
