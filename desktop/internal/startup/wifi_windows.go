//go:build windows

package startup

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	wlanDLL         = windows.NewLazySystemDLL("wlanapi.dll")
	wlanOpen        = wlanDLL.NewProc("WlanOpenHandle")
	wlanClose       = wlanDLL.NewProc("WlanCloseHandle")
	wlanEnum        = wlanDLL.NewProc("WlanEnumInterfaces")
	wlanProfiles    = wlanDLL.NewProc("WlanGetProfileList")
	wlanProfile     = wlanDLL.NewProc("WlanGetProfile")
	wlanConnect     = wlanDLL.NewProc("WlanConnect")
	wlanFree        = wlanDLL.NewProc("WlanFreeMemory")
	wifiIPHelper    = windows.NewLazySystemDLL("iphlpapi.dll")
	wifiGUIDToLUID  = wifiIPHelper.NewProc("ConvertInterfaceGuidToLuid")
	wifiLUIDToAlias = wifiIPHelper.NewProc("ConvertInterfaceLuidToAlias")
)

type nativeWiFiSession struct{ handle windows.Handle }
type nativeWiFiInterface struct {
	guid        windows.GUID
	description [256]uint16
	state       uint32
}
type nativeWiFiProfile struct {
	name  [256]uint16
	flags uint32
}
type nativeWiFiConnection struct {
	mode           uint32
	profile        *uint16
	ssid, bssid    uintptr
	bssType, flags uint32
}

func wifiAPIError(operation string, code uintptr) error {
	return fmt.Errorf("%s 失败：%w", operation, syscall.Errno(code))
}

func openWiFiSession() (wifiSession, error) {
	if err := wlanOpen.Find(); err != nil {
		return nil, fmt.Errorf("Windows WLAN API 不可用：%w", err)
	}
	var handle windows.Handle
	var version uint32
	code, _, _ := wlanOpen.Call(2, 0, uintptr(unsafe.Pointer(&version)), uintptr(unsafe.Pointer(&handle)))
	if code != 0 {
		return nil, fmt.Errorf("无法访问 WLAN AutoConfig 服务，请确认无线服务已启动：%w", syscall.Errno(code))
	}
	return &nativeWiFiSession{handle: handle}, nil
}

func (s *nativeWiFiSession) close() { wlanClose.Call(uintptr(s.handle), 0) }

func (s *nativeWiFiSession) interfaces() ([]wifiInterface, error) {
	var buffer unsafe.Pointer
	code, _, _ := wlanEnum.Call(uintptr(s.handle), 0, uintptr(unsafe.Pointer(&buffer)))
	if code != 0 {
		return nil, wifiAPIError("枚举无线网卡", code)
	}
	if buffer == nil {
		return nil, fmt.Errorf("无线网卡列表为空指针")
	}
	defer wlanFree.Call(uintptr(buffer))
	count := *(*uint32)(buffer)
	if count > 1024 {
		return nil, fmt.Errorf("无线网卡列表长度无效")
	}
	items := unsafe.Slice((*nativeWiFiInterface)(unsafe.Add(buffer, 8)), int(count))
	result := make([]wifiInterface, 0, len(items))
	for _, item := range items {
		var luid uint64
		code, _, _ = wifiGUIDToLUID.Call(uintptr(unsafe.Pointer(&item.guid)), uintptr(unsafe.Pointer(&luid)))
		if code != 0 {
			return nil, wifiAPIError("解析无线网卡标识", code)
		}
		var alias [257]uint16
		code, _, _ = wifiLUIDToAlias.Call(uintptr(unsafe.Pointer(&luid)), uintptr(unsafe.Pointer(&alias[0])), uintptr(len(alias)))
		if code != 0 {
			return nil, wifiAPIError("解析无线网卡名称", code)
		}
		result = append(result, wifiInterface{id: item.guid.String(), name: windows.UTF16ToString(alias[:]), state: item.state})
	}
	return result, nil
}

func (s *nativeWiFiSession) profiles(id string) ([]wifiProfile, error) {
	guid, err := windows.GUIDFromString(id)
	if err != nil {
		return nil, err
	}
	var buffer unsafe.Pointer
	code, _, _ := wlanProfiles.Call(uintptr(s.handle), uintptr(unsafe.Pointer(&guid)), 0, uintptr(unsafe.Pointer(&buffer)))
	if code != 0 {
		return nil, wifiAPIError("读取已保存无线网络列表", code)
	}
	if buffer == nil {
		return nil, fmt.Errorf("无线网络列表为空指针")
	}
	defer wlanFree.Call(uintptr(buffer))
	count := *(*uint32)(buffer)
	if count > 4096 {
		return nil, fmt.Errorf("无线网络列表长度无效")
	}
	items := unsafe.Slice((*nativeWiFiProfile)(unsafe.Add(buffer, 8)), int(count))
	result := make([]wifiProfile, 0, len(items))
	for _, item := range items {
		var payload *uint16
		var flags, access uint32 // Never request WLAN_PROFILE_GET_PLAINTEXT_KEY.
		code, _, _ = wlanProfile.Call(uintptr(s.handle), uintptr(unsafe.Pointer(&guid)), uintptr(unsafe.Pointer(&item.name[0])), 0, uintptr(unsafe.Pointer(&payload)), uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&access)))
		if code != 0 {
			continue
		} // An inaccessible profile must not hide other usable profiles.
		if payload == nil {
			continue
		}
		result = append(result, wifiProfile{name: windows.UTF16ToString(item.name[:]), xml: windows.UTF16PtrToString(payload)})
		wlanFree.Call(uintptr(unsafe.Pointer(payload)))
	}
	return result, nil
}

func (s *nativeWiFiSession) connect(id, profile string) error {
	guid, err := windows.GUIDFromString(id)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(profile)
	if err != nil {
		return err
	}
	params := nativeWiFiConnection{mode: 0, profile: name, bssType: 1}
	code, _, _ := wlanConnect.Call(uintptr(s.handle), uintptr(unsafe.Pointer(&guid)), uintptr(unsafe.Pointer(&params)), 0)
	// Read-only diagnosis: an organization/user policy must not be overwritten.
	key, policyErr := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Microsoft\Windows\WcmSvc\GroupPolicy`, registry.QUERY_VALUE)
	if policyErr == nil {
		value, _, readErr := key.GetIntegerValue("fMinimizeConnections")
		key.Close()
		if readErr == nil && value == 3 {
			return fmt.Errorf("Windows 同时连接策略为 3，有线已连接时禁止 WLAN；请在 Windows 连接管理器策略中检查，受管理设备请联系管理员")
		}
	}
	if code != 0 {
		return wifiAPIError("请求连接已保存的无线网络", code)
	}
	return nil
}
