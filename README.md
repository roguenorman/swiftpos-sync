# Swiftpos On-Prem to Cloud Supabase Sync Agent (Go 1.26 Native)

A production-grade, dependency-free stateless synchronization agent written in Go 1.26. It captures delta inventory updates from a local, on-premise Swiftpos Back Office API and pipes them securely to a cloud-hosted Supabase/Lovable frontend database.

## Architecture & Security Model

```text
[Local Swiftpos REST API] <--- Loopback ---> [Sync Agent (.exe)] -- Outbound HTTPS (443) --> [Supabase API Wrapper]
```

- **Outbound Ingress Only:** The agent operates completely as an outbound network client. No incoming firewall ports are modified, and no local APIs are exposed to the public internet.
- **Swiftpos as Content Master:** Product names and web-ready marketing copies are authored inside the Swiftpos Back Office product window via the **Web Notes** pane, making it the single source of truth for all transactional and text content.
- **Stateless High-Water Mark Processing:** The agent queries the cloud database for the newest `updated_at` timestamp and asks Swiftpos for modifications since that checkpoint (`?modifiedSince=`).
- **Resilience Engineering:** Built-in exponential network backoff, context execution limits, automated payload chunking (blocks of 500 rows), and self-healing log management.

## Project Structure

```text
swiftpos-sync/
├── go.mod        # Toolchain definition constraints
├── config.go     # Protected structural file reader
├── logger.go     # Self-truncating logging loop (10MB Cap)
├── main.go       # Core sync worker orchestration engine
├── Makefile      # Build runner automation contract
└── README.md     # Production deployment playbook
```

## Compilation

Ensure your machine is running **Go 1.26** or newer.

### From Linux or macOS Dev Machine
```bash
# Compile for both production targets
make all

# Target the Windows on-prem host machine specifically
make build-windows
```

### From a Windows Dev Machine
```powershell
go build -ldflags="-s -w" -o build/swiftpos_sync.exe .
```

---

## Production Deployment Playbook

### Step 1: Provision System Directories
On the dedicated local on-prem Windows Server, establish the secure application directory root:
```powershell
New-Item -ItemType Directory -Path "C:\Program Files\SwiftposSync"
```
Move the compiled `swiftpos_sync.exe` binary into this folder.

### Step 2: Create and Secure Configuration
Generate a file named `C:\Program Files\SwiftposSync\config.json` containing the access credentials:

```json
{
  "pos_api_url": "http://localhost:8000/api/Product",
  "supabase_url": "https://supabase.co",
  "supabase_api_key": "YOUR_SECRET_SERVICE_ROLE_JWT"
}
```

Run these commands in **PowerShell as Administrator** to secure the credentials via NTFS ACL rule overrides so only system-level accounts can view them:

```powershell
\$Path = "C:\Program Files\SwiftposSync\config.json"

# Disable inheritance and copy current permissions as explicit rules
\$Acl = Get-Acl \(Path\)Acl.SetAccessRuleProtection(\(true,\)true)
Set-Acl \(Path\)Acl

# Remove unprivileged users and everyone strings completely
\$Acl = Get-Acl \(Path\)Acl.Access | Where-Object { \(_.IdentityReference -like "*Users*" -or \)_.IdentityReference -like "*Everyone*" } | ForEach-Object {
    \(Acl.RemoveAccessRule(\)_)
}
Set-Acl \(Path\)Acl

# Force explicit full control limits for SYSTEM and Administrators
icacls \$Path /grant:r "NT AUTHORITY\SYSTEM:(F)"
icacls \$Path /grant:r "BUILTIN\Administrators:(F)"
```

### Step 3: Automate Execution via Windows Task Scheduler ("Cron Job")
Windows handles background task loops natively via the **Task Scheduler subsystem**. This replaces the Linux `cron` daemon, offering clean process lifecycle limits, recovery structures, and system boot bindings.

Run this PowerShell code block as an **Administrator** to deploy the agent task. It registers a task that boots immediately on startup and loops every **5 minutes** indefinitely, without requiring any interactive users to be logged in.

```powershell
# 1. Define the exact execution binary path targeting local program space
\$Action = New-ScheduledTaskAction -Execute "C:\Program Files\SwiftposSync\swiftpos_sync.exe"

# 2. Establish a persistent repetition trigger interval (5 minutes)
\$Trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 5)

# 3. Apply defensive runtime constraints to stop hanging threads or zombie leaks
\$Settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 3)

# 4. Commit the task to run securely as the isolated background System service
Register-ScheduledTask -TaskName "SwiftposCloudSync_Prod" -Action \$Action -Trigger \(trigger -Settings\)settings -User "NT AUTHORITY\SYSTEM"
```

## Monitoring & Logs

The binary writes log output directly to `C:\Program Files\SwiftposSync\sync_agent.log`. 
- Every run will log its operational status and the count of synchronization payloads processed.
- If the log exceeds **10MB**, the internal logger automatically truncates it on the next run to prevent server disk space exhaustion.
