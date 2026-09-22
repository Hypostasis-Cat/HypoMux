//go:build windows

package services

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHotspotWindowsSharingVerdictAndPrivateReadiness(t *testing.T) {
	runHotspotFunctions(t, `
$publicID = [guid]'00000000-0000-0000-0000-000000000001'
$script:privateID = [guid]'00000000-0000-0000-0000-000000000002'
$otherID = [guid]'00000000-0000-0000-0000-000000000003'
if ((Get-SharingVerdict @() $publicID) -ne 'unobserved') { throw 'empty sharing must be inconclusive' }
$public = [PSCustomObject]@{ Guid = $publicID; Role = 0 }
$private = [PSCustomObject]@{ Guid = $script:privateID; Role = 1 }
if ((Get-SharingVerdict @($public, $private) $publicID) -ne 'verified') { throw 'matching pair rejected' }
$public.Guid = $otherID
if ((Get-SharingVerdict @($public, $private) $publicID) -ne 'mismatch') { throw 'wrong public accepted' }
$public.Guid = $publicID
$private.Guid = $otherID
if ((Get-SharingVerdict @($public, $private) $publicID) -ne 'mismatch') { throw 'wrong private accepted' }
if ((Get-SharingVerdict @($public) $publicID) -ne 'transitional') { throw 'partial sharing must retry' }
$manager = [PSCustomObject]@{ TetheringOperationalState = 'On'; ClientCount = 0 }
$fakeAddress = '192.168.137.1'
$fakeState = 'Up'
function Get-NetAdapter { param([switch]$IncludeHidden) [PSCustomObject]@{ InterfaceDescription = 'Microsoft Wi-Fi Direct Virtual Adapter'; Status = $fakeState; InterfaceGuid = '00000000-0000-0000-0000-000000000002'; ifIndex = 23 } }
function Get-NetIPAddress { param($InterfaceIndex, $AddressFamily, $ErrorAction) [PSCustomObject]@{ InterfaceIndex = 23; AddressState = 'Preferred'; IPAddress = $fakeAddress } }
function Get-NetRoute { param($AddressFamily, $ErrorAction) @() }
$script:privateID = $null
if (-not (Test-ReadyNetwork)) { throw 'new operational AP rejected' }
if ($script:gatewayAddress -ne '192.168.137.1') { throw 'gateway missing' }
$fakeAddress = '169.254.1.2'
if (Test-ReadyNetwork) { throw 'APIPA accepted' }
$fakeAddress = '192.168.137.1'
$fakeState = 'Disconnected'
if (Test-ReadyNetwork) { throw 'disconnected AP accepted' }
$fakeState = 'Up'
$script:privateID = $null
Save-NetworkBaseline
if (Test-ReadyNetwork) { throw 'pre-existing AP accepted' }
# Reproduce the report: vendor-named WLAN 12 becomes Up while WLAN and WLAN 11
# remain connected. No adapter description contains Wi-Fi Direct.
$apID = [guid]'00000000-0000-0000-0000-000000000002'
$ap = [PSCustomObject]@{ Name = 'WLAN 12'; InterfaceDescription = 'MediaTek Wi-Fi 7 MT7927 Wireless LAN Card'; InterfaceType = 71; NdisPhysicalMedium = 9; Status = 'Disconnected'; InterfaceGuid = $apID.ToString('B'); ifIndex = 13 }
$uplink = [PSCustomObject]@{ Name = 'WLAN 11'; InterfaceDescription = $ap.InterfaceDescription; InterfaceType = 71; NdisPhysicalMedium = 9; Status = 'Up'; InterfaceGuid = $otherID; ifIndex = 9 }
$qualcomm = [PSCustomObject]@{ Name = 'WLAN'; InterfaceDescription = 'Qualcomm FastConnect 7800 Wi-Fi 7'; InterfaceType = 71; Status = 'Up'; InterfaceGuid = $publicID; ifIndex = 29 }
$fakeAdapters = @($ap, $uplink, $qualcomm)
$fakeRoutes = @([PSCustomObject]@{ DestinationPrefix = '0.0.0.0/0'; InterfaceIndex = 9 })
$addressState = 'Preferred'
function Get-NetAdapter { param([switch]$IncludeHidden) $fakeAdapters }
function Get-NetRoute { param($AddressFamily, $ErrorAction) $fakeRoutes }
function Get-NetIPAddress { param($InterfaceIndex, $AddressFamily, $ErrorAction) $fakeAdapters | ForEach-Object { [PSCustomObject]@{ InterfaceIndex = $_.ifIndex; AddressState = $addressState; IPAddress = $fakeAddress } } }
Save-NetworkBaseline
if (Test-ReadyNetwork) { throw 'disconnected vendor AP accepted' }
$ap.Status = 'Up'
if (-not (Test-ReadyNetwork)) { throw 'vendor-named Wi-Fi 7 AP rejected' }
if ($script:privateID -ne $apID -or $script:gatewayAddress -ne '192.168.137.1') { throw 'wrong vendor AP selected' }
if (-not (Test-ReadyNetwork)) { throw 'pinned vendor AP rejected by watchdog' }
$addressState = 'Tentative'
if (Test-ReadyNetwork) { throw 'tentative vendor AP address accepted' }
$addressState = 'Preferred'
$fakeAddress = '192.168.173.1'
if (-not (Test-ReadyNetwork)) { throw 'non-default hotspot subnet rejected' }
$fakeRoutes += [PSCustomObject]@{ DestinationPrefix = '0.0.0.0/0'; InterfaceIndex = 13 }
if (Test-ReadyNetwork) { throw 'AP acquiring an upstream default route accepted' }
$script:privateID = $null
if (Test-ReadyNetwork) { throw 'new Wi-Fi uplink accepted as AP' }
$fakeRoutes = @()
$ap.InterfaceType = 6
$ap.NdisPhysicalMedium = 14
if (Test-ReadyNetwork) { throw 'new Ethernet adapter accepted as AP' }
$ap.NdisPhysicalMedium = 9
if (-not (Test-ReadyNetwork)) { throw 'native wireless medium fallback rejected' }
$script:privateID = $null
$script:beforeNetwork.Remove($otherID.ToString())
if (Test-ReadyNetwork) { throw 'ambiguous new wireless adapters accepted' }
$script:privateID = $apID
$fakeAdapters = @($uplink, $qualcomm)
if (Test-ReadyNetwork) { throw 'watchdog switched to another wireless adapter' }
$manager.TetheringOperationalState = 'Off'
if (Test-ReadyNetwork) { throw 'stopped hotspot accepted' }
if ((Get-StartFailure 'WiFiDeviceOff') -notlike '*WiFiDeviceOff*') { throw 'radio error code lost' }
$manager | Add-Member ScriptMethod StopTetheringAsync { throw 'must not stop an already off hotspot' }
Stop-OwnedHotspot
$manager.TetheringOperationalState = 'On'
$manager | Add-Member -Force ScriptMethod StopTetheringAsync { $this.TetheringOperationalState = 'Off'; throw 'late stop error' }
Stop-OwnedHotspot
$manager.TetheringOperationalState = 'On'
$manager | Add-Member -Force ScriptMethod StopTetheringAsync { throw 'real stop failure' }
$rejected = $false
try { Stop-OwnedHotspot } catch { $rejected = $true }
if (-not $rejected) { throw 'live cleanup failure was suppressed' }
$fakeAdapters = @([PSCustomObject]@{ InterfaceGuid = '{00000000-0000-0000-0000-000000000001}'; Status = 'Up' })
function Get-NetAdapter { param([switch]$IncludeHidden) $fakeAdapters }
if (-not (Test-PublicNetwork $publicID)) { throw 'running TUN with braced string GUID rejected' }
$fakeAdapters[0].InterfaceGuid = $publicID
if (-not (Test-PublicNetwork $publicID)) { throw 'running TUN with typed GUID rejected' }
$fakeAdapters[0].Status = 'Disconnected'
if (Test-PublicNetwork $publicID) { throw 'disconnected TUN accepted' }
$fakeAdapters[0].Status = 'Up'
$fakeAdapters[0].InterfaceGuid = $otherID
if (Test-PublicNetwork $publicID) { throw 'different adapter accepted as TUN' }
$fakeAdapters = @()
if (Test-PublicNetwork $publicID) { throw 'missing TUN accepted' }
$manager = [PSCustomObject]@{ ClientCount = 1 }
$manager | Add-Member ScriptMethod GetTetheringClients { @([PSCustomObject]@{ MacAddress = 'AA:BB:CC:DD:EE:FF'; HostNames = @([PSCustomObject]@{ DisplayName = 'test-phone' }) }) }
$output = New-Object System.IO.StringWriter
$originalOutput = [Console]::Out
try {
    [Console]::SetOut($output)
    Publish-State 'running' '' $false
    $state = $output.ToString() | ConvertFrom-Json
    if (-not $state.devices_available -or $state.devices[0].hosts[0] -ne 'test-phone') { throw 'client details missing' }
    $output.GetStringBuilder().Clear() | Out-Null
    $manager | Add-Member -Force ScriptMethod GetTetheringClients { throw 'unsupported client details' }
    Publish-State 'running' '' $false
    $state = $output.ToString() | ConvertFrom-Json
    if ($state.devices_available -or $state.state -ne 'running' -or $state.clients -ne 1) { throw 'optional client failure affected status' }
} finally { [Console]::SetOut($originalOutput); $output.Dispose() }
`)
}

func TestHotspotWindowsReusedInterfaceAndStability(t *testing.T) {
	runHotspotFunctions(t, `
$manager = [PSCustomObject]@{ TetheringOperationalState = 'On' }
$id = [guid]'00000000-0000-0000-0000-000000000002'
$address = @(); $routes = @(); $failRoute = $false
function Get-NetAdapter { [PSCustomObject]@{ InterfaceGuid = $id; ifIndex = 13; Status = 'Up'; InterfaceType = 71 } }
function Get-NetIPAddress { $address }
function Get-NetRoute { if ($failRoute) { throw 'temporary CIM failure' }; $routes }
Save-NetworkBaseline
$address = @([PSCustomObject]@{ InterfaceIndex = 13; AddressState = 'Preferred'; IPAddress = '192.168.137.1' })
if (Test-PrivateNetwork) { throw 'first sample pinned too early' }
$routes = @([PSCustomObject]@{ InterfaceIndex = 13; DestinationPrefix = '0.0.0.0/0' })
if (Test-PrivateNetwork) { throw 'delayed upstream route accepted' }
if ($null -ne $script:privateID) { throw 'unstable candidate was pinned' }
$routes = @()
if (Test-PrivateNetwork) { throw 'stability did not reset' }
$failRoute = $true
if (Test-PrivateNetwork) { throw 'failed query accepted' }
if (-not $script:networkQueryFailed) { throw 'query error not distinguished' }
$failRoute = $false
if (Test-PrivateNetwork) { throw 'query failure did not reset stability' }
if (Test-PrivateNetwork) { throw 'second sample pinned too early' }
if (-not (Test-PrivateNetwork) -or $script:privateID -ne $id) { throw 'already Up addressless interface rejected' }
$script:privateID = $null
Save-NetworkBaseline
if (Test-ReadyNetwork) { throw 'preexisting addressed interface accepted' }
`)
}

func TestHotspotWindowsSharingRetryAndCadence(t *testing.T) {
	runHotspotFunctions(t, `
$id = [guid]'00000000-0000-0000-0000-000000000001'
$answer = 'transitional'
function Test-Sharing { param($publicID) $answer }
if ((Update-SharingCheck $id) -ne 'transitional') { throw 'partial snapshot not retried' }
$answer = 'verified'
$null = Update-SharingCheck $id
if ($script:sharingRetryCount -ne 0 -or $script:nextSharingCheck -lt [DateTime]::UtcNow.AddSeconds(14)) { throw 'normal cadence or reset incorrect' }
foreach ($answer in @('query_error', 'transitional')) {
 $script:sharingRetryCount = 0
 $null = Update-SharingCheck $id
 $null = Update-SharingCheck $id
 $rejected = $false
 try { Update-SharingCheck $id } catch { $rejected = $true }
 if (-not $rejected) { throw 'persistent failure accepted' }
}
$script:sharingRetryCount = 0
$answer = 'mismatch'
$rejected = $false
try { Update-SharingCheck $id } catch { $rejected = $true }
if (-not $rejected) { throw 'wrong egress not rejected immediately' }
`)
}

func TestHotspotWindowsCleanupOutcomes(t *testing.T) {
	runHotspotFunctions(t, `
$attempted = $true; $configured = $true; $original = 'old'
$manager = [PSCustomObject]@{}
$manager | Add-Member ScriptMethod ConfigureAccessPointAsync { param($value) $script:restoreCalls++; throw 'restore failure' }
$restoreCalls = 0
function Stop-OwnedHotspot { throw 'stop failure' }
Complete-HotspotCleanup
if (-not $cleanupFailed -or $hotspotOffConfirmed -eq $true -or $restoreCalls -ne 0) { throw 'unconfirmed shutdown restored live configuration' }
$cleanupFailed = $false; $cleanupError = ''; $failure = ''; $configurationRestored = $false
function Stop-OwnedHotspot { }
Complete-HotspotCleanup
if (-not $hotspotOffConfirmed -or $configurationRestored -or -not $cleanupFailed -or $restoreCalls -ne 1) { throw 'restore failure confused with shutdown failure' }
$cleanupFailed = $false; $cleanupError = ''; $failure = ''
$manager | Add-Member -Force ScriptMethod ConfigureAccessPointAsync { param($value) return 'done' }
function Await-Action { param($operation) }
Complete-HotspotCleanup
if (-not $hotspotOffConfirmed -or -not $configurationRestored -or $cleanupFailed) { throw 'successful cleanup reported as failure' }
`)
}

func runHotspotFunctions(t *testing.T, body string) {
	t.Helper()
	executable, err := resolveWindowsPowerShellExecutable()
	if err != nil {
		t.Fatal(err)
	}
	// Execute production functions against fakes, without changing networking.
	functions := strings.Split(strings.ReplaceAll(hotspotScript, "\r\n", "\n"), "\ntry {\n")[0]
	script := functions + "\nfunction Test-ReadyNetwork { 1..2 | ForEach-Object { $null = Test-PrivateNetwork }; Test-PrivateNetwork }\n" + body
	path := filepath.Join(t.TempDir(), "hotspot-test.ps1")
	if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte(script)...), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path)
	configureBackgroundCommand(command)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
}

func TestHotspotWindowsIgnoresUnreadySiblingInterfaces(t *testing.T) {
	runHotspotFunctions(t, `
$manager = [PSCustomObject]@{ TetheringOperationalState = 'On' }
$apID = [guid]'00000000-0000-0000-0000-000000000002'
$siblingID = [guid]'00000000-0000-0000-0000-000000000003'
$adapters = @(
    [PSCustomObject]@{ InterfaceGuid = $apID; ifIndex = 13; Status = 'Up'; InterfaceType = 71 },
    [PSCustomObject]@{ InterfaceGuid = $siblingID; ifIndex = 14; Status = 'Up'; InterfaceType = 71 }
)
$siblingAddress = '169.254.33.2'
$siblingState = 'Preferred'
function Get-NetAdapter { param([switch]$IncludeHidden) $adapters }
function Get-NetRoute { param($AddressFamily, $ErrorAction) @() }
function Get-NetIPAddress {
    param($InterfaceIndex, $AddressFamily, $ErrorAction)
    [PSCustomObject]@{ InterfaceIndex = 13; AddressState = 'Preferred'; IPAddress = '192.168.137.1' }
    if ($siblingAddress) { [PSCustomObject]@{ InterfaceIndex = 14; AddressState = $siblingState; IPAddress = $siblingAddress } }
}
foreach ($address in @('169.254.33.2', '0.0.0.0', '127.0.0.1', '')) {
    $siblingAddress = $address
    $script:privateID = $null
    if (-not (Test-ReadyNetwork) -or $script:privateID -ne $apID) { throw 'unready sibling blocked valid AP' }
}
$siblingAddress = '192.168.173.1'
$siblingState = 'Tentative'
$script:privateID = $null
if (-not (Test-ReadyNetwork)) { throw 'tentative sibling blocked valid AP' }
$siblingState = 'Preferred'
if (-not (Test-ReadyNetwork) -or $script:privateID -ne $apID) { throw 'watchdog lost pinned AP when sibling became ready' }
$script:privateID = $null
if (Test-ReadyNetwork) { throw 'two ready AP candidates accepted' }
$script:privateID = $apID
$adapters = @($adapters[1])
if (Test-ReadyNetwork) { throw 'missing pinned AP replaced by sibling' }
`)
}

func TestHotspotWindowsTunProfileDiscovery(t *testing.T) {
	runHotspotFunctions(t, `
$tunID = [guid]'00000000-0000-0000-0000-000000000001'
$otherID = [guid]'00000000-0000-0000-0000-000000000002'
$tunAdapter = [PSCustomObject]@{ NetworkAdapterId = $tunID.ToString('B') }
# Windows 10 can reuse the upstream profile name for the TUN. Identity must
# come from the adapter GUID, even when both profiles have identical names.
$tunProfile = [PSCustomObject]@{ ProfileName = 'Upstream Wi-Fi'; NetworkAdapter = $tunAdapter }
$otherProfile = [PSCustomObject]@{ ProfileName = 'Upstream Wi-Fi'; NetworkAdapter = [PSCustomObject]@{ NetworkAdapterId = $otherID } }
$observed = @($otherProfile, $tunProfile)
function Read-ConnectionProfiles { $observed }
function Read-HostNetworkAdapters { throw 'direct query unnecessary' }
if ((Read-TunProfile $tunID) -ne $tunProfile) { throw 'matching global profile rejected' }
$observed = @($tunProfile, $tunProfile)
$rejected = $false
try { Read-TunProfile $tunID } catch { $rejected = $true }
if (-not $rejected) { throw 'ambiguous global profiles accepted' }
$observed = @($otherProfile)
$tunAdapter | Add-Member ScriptMethod GetConnectedProfileAsync { $script:directCalls++; return $script:directProfile }
$script:directCalls = 0
$script:directProfile = $tunProfile
function Read-HostNetworkAdapters { @($otherProfile.NetworkAdapter, $tunAdapter, $tunAdapter) }
function Await-Operation($operation, [Type]$resultType, [int]$timeoutMs) { return $operation }
if ((Read-TunProfile $tunID) -ne $tunProfile -or $script:directCalls -ne 1) { throw 'same-adapter direct lookup failed' }
if ($script:sharingDetail -notlike '*adapter_connected_profile*') { throw 'direct source missing from diagnostics' }
$script:directProfile = $otherProfile
$rejected = $false
try { Read-TunProfile $tunID } catch { $rejected = $true }
if (-not $rejected) { throw 'direct query accepted another adapter' }
$script:directProfile = $null
if ($null -ne (Read-TunProfile $tunID)) { throw 'null direct profile accepted' }
function Read-HostNetworkAdapters { @($otherProfile.NetworkAdapter) }
if ($null -ne (Read-TunProfile $tunID)) { throw 'physical profile used as fallback' }
$script:stopSignal = [PSCustomObject]@{ IsCompleted = $false }
$script:stopSignal | Add-Member ScriptMethod Wait { param($timeout) return $this.IsCompleted }
function Test-PublicNetwork { param($id) return $true }
$script:reads = 0
function Read-ConnectionProfiles { $script:reads++; if ($script:reads -ge 3) { $tunProfile } else { throw 'transient enumeration failure' } }
if ((Wait-TunProfile $tunID) -ne $tunProfile -or $script:reads -ne 3) { throw 'delayed profile not retried' }
function Read-ConnectionProfiles { @() }
$message = ''
try { Wait-TunProfile $tunID 0 } catch { $message = $_.Exception.Message }
if ($message -notlike '*HypoMux-Tun*' -or $message -notlike '*共享诊断*') { throw 'persistent absence lacks actionable error' }
$script:stopSignal.IsCompleted = $true
$message = ''
try { Wait-TunProfile $tunID } catch { $message = $_.Exception.Message }
if ($message -notlike '*Desktop closed*') { throw 'discovery ignored shutdown' }
$script:stopSignal.IsCompleted = $false
function Test-PublicNetwork { param($id) return $false }
$message = ''
try { Wait-TunProfile $tunID } catch { $message = $_.Exception.Message }
if ($message -notlike '*stopped during*') { throw 'discovery ignored TUN disappearance' }
`)
}

func TestHotspotWindowsBandAPIVersionCompatibility(t *testing.T) {
	runHotspotFunctions(t, `
foreach ($status in @('WiFiDeviceOff', 'RadioRestriction', 'BandInterference')) {
    $message = Get-StartFailure $status '5'
    if (-not $message.Contains($status) -or $message -notlike '*5 GHz*' -or $message -notlike '*自动频段*') { throw '5 GHz failure lacks actionable context' }
}

foreach ($band in @('auto', '2.4')) {
    $message = Get-StartFailure 'WiFiDeviceOff' $band
    if ($message -notlike '*WiFiDeviceOff*' -or $message -like '*指定的 5 GHz*') { throw 'radio failure incorrectly attributed to 5 GHz' }
}
if ((Get-StartFailure 'Unknown' '5') -ne 'Windows tethering status: Unknown') { throw 'unrelated failure incorrectly attributed to band' }
$legacy = [PSCustomObject]@{ Ssid = 'test' }
Set-HotspotBand $legacy 'auto' $false
foreach ($band in @('2.4', '5')) {
    $message = ''
    try { Set-HotspotBand $legacy $band $false } catch { $message = $_.Exception.Message }
    if ($message -notlike '*Windows 10 2004*') { throw 'missing band API did not give actionable error' }
}
$modern = [PSCustomObject]@{ Band = $null }
$modern | Add-Member ScriptMethod IsBandSupported { param($band) return $true }
foreach ($band in @('auto', '2.4', '5')) {
    Set-HotspotBand $modern $band $true
    $expected = @{ auto = 'Auto'; '2.4' = 'TwoPointFourGigahertz'; '5' = 'FiveGigahertz' }[$band]
    if ([string]$modern.Band -ne $expected) { throw 'wrong band selected' }
}
$modern | Add-Member -Force ScriptMethod IsBandSupported { param($band) return $false }
$rejected = $false
try { Set-HotspotBand $modern '5' $true } catch { $rejected = $true }
if (-not $rejected) { throw 'unsupported hardware band accepted' }
`)
}

func TestHotspotWindowsAutomaticBandStartup(t *testing.T) {
	runHotspotFunctions(t, `
$desired = [PSCustomObject]@{ Band = $null }
$desired | Add-Member ScriptMethod IsBandSupported { param($band) return $true }
if ((Select-StartupBand $desired 'auto' $true) -ne '5') { throw 'automatic did not prefer 5 GHz' }
if ((Select-StartupBand $desired '2.4' $true) -ne '2.4') { throw 'explicit band changed' }
if ((Select-StartupBand $desired 'auto' $false) -ne 'auto') { throw 'legacy fallback lost' }
$desired | Add-Member -Force ScriptMethod IsBandSupported { param($band) return $false }
if ((Select-StartupBand $desired 'auto' $true) -ne 'auto') { throw 'unsupported 5 GHz preferred' }
$desired | Add-Member -Force ScriptMethod IsBandSupported { param($band) throw 'query unavailable' }
if ((Select-StartupBand $desired 'auto' $true) -ne 'auto') { throw 'query failure broke automatic' }
$desired | Add-Member -Force ScriptMethod IsBandSupported { param($band) return $true }
$resultType = [object]
$stopSignal = [PSCustomObject]@{ IsCompleted = $false }
function Await-Action($operation) { }
function Await-Operation($operation, [Type]$resultType) { return $operation }
foreach ($case in @(
    @{ Requested = 'auto'; Failure = 'Success'; State = 'Off'; Calls = 1; FinalBand = '5' },
    @{ Requested = 'auto'; Failure = 'BandInterference'; State = 'Off'; Calls = 2; FinalBand = 'auto' },
    @{ Requested = 'auto'; Failure = 'RadioRestriction'; State = 'Off'; Calls = 2; FinalBand = 'auto' },
    @{ Requested = 'auto'; Failure = 'WiFiDeviceOff'; State = 'Off'; Calls = 2; FinalBand = 'auto' },
    @{ Requested = '5'; Failure = 'BandInterference'; State = 'Off'; Calls = 1; FinalBand = '5' },
    @{ Requested = 'auto'; Failure = 'Unknown'; State = 'Off'; Calls = 1; FinalBand = '5' },
    @{ Requested = 'auto'; Failure = 'BandInterference'; State = 'On'; Calls = 1; FinalBand = '5' },
    @{ Requested = 'auto'; Failure = 'BandInterference'; State = 'InTransition'; Calls = 1; FinalBand = '5' }
)) {
    $script:starts = 0; $script:configures = 0
    $manager = [PSCustomObject]@{ TetheringOperationalState = $case.State }
    $manager | Add-Member ScriptMethod ConfigureAccessPointAsync { param($value) $script:configures++ }
    $manager | Add-Member ScriptMethod StartTetheringAsync {
        $script:starts++
        if ($script:starts -eq 1) { return [PSCustomObject]@{ Status = $case.Failure } }
        return [PSCustomObject]@{ Status = 'Success' }
    }
    $failed = $false
    try { Start-ConfiguredHotspot $desired $case.Requested $true } catch { $failed = $true }
    $shouldFail = $case.Calls -eq 1 -and $case.Failure -ne 'Success'
    if ($failed -ne $shouldFail -or $script:starts -ne $case.Calls -or $script:configures -ne $case.Calls -or $configuredBand -ne $case.FinalBand) { throw ('unexpected startup: ' + ($case | ConvertTo-Json -Compress)) }
    if ($case.Calls -eq 2 -and $bandFallback -ne $case.Failure) { throw 'fallback reason lost' }
}
$stopSignal.IsCompleted = $true
$script:starts = 0
try { Start-ConfiguredHotspot $desired 'auto' $true } catch { }
if ($script:starts -ne 0) { throw 'started after shutdown request' }
`)
}

// Only exercise the real worker's preflight rejection. Never start a hotspot
// from an automated test, even on a machine with an active HypoMux TUN.
func TestHotspotWindowsRejectsMissingTUNWithoutMutation(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range interfaces {
		if adapter.Name == "HypoMux-Tun" {
			t.Skip("active TUN; do not mutate live networking")
		}
	}
	command, err := hotspotCommand()
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h, err := launchHotspot(ctx, command, HotspotConfig{SSID: "HypoMux-test", Password: "test-password", Band: "auto"})
	if err == nil {
		_ = h.stop(ctx)
		t.Fatal("worker unexpectedly accepted missing TUN")
	}
	if !strings.Contains(err.Error(), "TUN profile discovery") || !strings.Contains(err.Error(), "HypoMux-Tun") {
		t.Fatal(err)
	}
	if !h.snapshot().CleanupComplete {
		t.Fatal("preflight must exit cleanly", h.snapshot())
	}
}
