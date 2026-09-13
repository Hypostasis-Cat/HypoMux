//go:build windows

package platform

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Exercise the PowerShell COM-call shape using method-only objects. Treating
// EnumEveryConnection as a property must not silently yield an empty snapshot.
func TestSharingEnumerationInvokesMethodAndPreservesRoles(t *testing.T) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Split(sharingInspectionScript, "$sharing = New-Object -ComObject")[0] + `
$fake = New-Object PSObject
$fake | Add-Member ScriptMethod EnumEveryConnection { return @(0, 1, 2) }
$fake | Add-Member ScriptMethod INetSharingConfigurationForINetConnection { param($id) return [PSCustomObject]@{ SharingEnabled = ($id -ne 2); SharingConnectionType = $id } }
$fake | Add-Member ScriptMethod NetConnectionProps { param($id) return [PSCustomObject]@{ Guid = ('00000000-0000-0000-0000-00000000000' + $id); Name = ('adapter-' + $id) } }
@{ connections = @(Read-SharedConnections $fake) } | ConvertTo-Json -Depth 4 -Compress
`
	output, err := exec.Command(filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	var snapshot SharingSnapshot
	if err := json.Unmarshal(output, &snapshot); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if len(snapshot.Connections) != 2 || snapshot.Connections[0].Role != 0 || snapshot.Connections[1].Role != 1 || snapshot.Connections[0].Name != "adapter-0" {
		t.Fatalf("unexpected enumeration: %+v", snapshot)
	}
}
