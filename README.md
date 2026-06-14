# Define the operational execution logic and point errors out to clean log files
$executable = "C:\Program Files\SwiftposSync\swiftpos_sync.exe"
$argument   = ">> `"C:\Program Files\SwiftposSync\sync_output.log`" 2>> `"C:\Program Files\SwiftposSync\sync_errors.log`""

# Force system to initialize the binary via cmd context to accurately trap the log streams
$action = New-ScheduledTaskAction -Execute "cmd.exe" -Argument "/c $executable $argument"
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 1)

# Task settings for reliability
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 2)

# Commit task directly to the secure system runtime container
Register-ScheduledTask -TaskName "SwiftposCloudSync_Prod" -Action $action -Trigger $trigger -Settings $settings -User "NT AUTHORITY\SYSTEM"

