//go:build windows

package wfp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	fwpUint8                       = 1
	fwpUint16                      = 2
	fwpUint32                      = 3
	fwpByteBlobType                = 12
	fwpMatchEqual                  = 0
	fwpActionPermit                = 0x00001002
	fwpmSessionFlagDynamic         = 0x00000001
	fwpmFilterFlagClearActionRight = 0x00000008
	rpcCAuthnDefault               = ^uint32(0)
	ipProtoTCP                     = 6
	ipProtoUDP                     = 17
)

var (
	layerALEAuthConnectV4 = mustGUID("c38d57d1-05a7-4c33-904f-7fbceee60e82")
	conditionALEAppID     = mustGUID("d78e1e87-8644-4ea5-9437-d809ecefc971")
	conditionLocalAddress = mustGUID("d9ee00de-c1ef-4617-bfe3-ffd8f5a08957")
	conditionInterface    = mustGUID("667fd755-d695-434a-8af5-d3835a1259bc")
	conditionIPProtocol   = mustGUID("3971ef2b-623e-4f9a-8cb1-6e79b806b9a7")
	conditionRemotePort   = mustGUID("c35a604d-d22b-4e1a-91b4-68f674ee674b")
)

type displayData struct {
	Name        *uint16
	Description *uint16
}

type byteBlob struct {
	Size uint32
	_    uint32
	Data *byte
}

type value struct {
	Type  uint32
	_     uint32
	Value uintptr
}

type filterCondition struct {
	FieldKey       windows.GUID
	MatchType      uint32
	_              uint32
	ConditionValue value
}

type action struct {
	Type       uint32
	FilterType windows.GUID
}

type filterContext struct {
	RawContext uint64
	Extra      uint64
}

type filter struct {
	FilterKey           windows.GUID
	DisplayData         displayData
	Flags               uint32
	_                   uint32
	ProviderKey         *windows.GUID
	ProviderData        byteBlob
	LayerKey            windows.GUID
	SubLayerKey         windows.GUID
	Weight              value
	NumFilterConditions uint32
	_                   uint32
	FilterCondition     *filterCondition
	Action              action
	_                   uint32
	Context             filterContext
	Reserved            *windows.GUID
	FilterID            uint64
	EffectiveWeight     value
}

type session struct {
	SessionKey         windows.GUID
	DisplayData        displayData
	Flags              uint32
	TxnWaitTimeoutMSec uint32
	ProcessID          uint32
	_                  uint32
	SID                uintptr
	Username           *uint16
	KernelMode         int32
	_                  uint32
}

type subLayer struct {
	SubLayerKey  windows.GUID
	DisplayData  displayData
	Flags        uint32
	_            uint32
	ProviderKey  *windows.GUID
	ProviderData byteBlob
	Weight       uint16
	_            [6]byte
}

type dnsRule struct {
	adapter  string
	sourceIP uint32
	ifIndex  uint32
	protocol uint8
}

type dnsSession struct {
	mu        sync.Mutex
	engine    windows.Handle
	filterIDs []uint64
}

type api struct {
	engineOpen        *windows.LazyProc
	engineClose       *windows.LazyProc
	subLayerAdd       *windows.LazyProc
	getAppID          *windows.LazyProc
	transactionBegin  *windows.LazyProc
	transactionCommit *windows.LazyProc
	transactionAbort  *windows.LazyProc
	filterAdd         *windows.LazyProc
	freeMemory        *windows.LazyProc
}

func OpenDNSExemption(applicationPath string, adapters []Adapter) (DNSExemption, error) {
	rules := buildRules(adapters)
	if len(rules) == 0 {
		return nil, errors.New("no selected adapter has a usable IPv4 address and interface index")
	}
	if strings.TrimSpace(applicationPath) == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locate Core executable for WFP application identity: %w", err)
		}
		applicationPath = executable
	}
	absolute, err := filepath.Abs(applicationPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("invalid WFP application identity path %q", absolute)
	}
	wfp := newAPI()
	sessionKey, err := windows.GenerateGUID()
	if err != nil {
		return nil, err
	}
	sessionName, _ := windows.UTF16PtrFromString("HypoMux temporary DNS egress exemption")
	sessionDescription, _ := windows.UTF16PtrFromString(
		"Dynamic, per-adapter TCP/UDP 53 permit for HypoMux Core upstream sockets",
	)
	sessionData := session{
		SessionKey:  sessionKey,
		DisplayData: displayData{Name: sessionName, Description: sessionDescription},
		Flags:       fwpmSessionFlagDynamic,
	}
	var engine windows.Handle
	if err := wfp.call(
		wfp.engineOpen,
		"FwpmEngineOpen0",
		0,
		uintptr(rpcCAuthnDefault),
		0,
		uintptr(unsafe.Pointer(&sessionData)),
		uintptr(unsafe.Pointer(&engine)),
	); err != nil {
		return nil, err
	}
	owned := &dnsSession{engine: engine}
	success := false
	defer func() {
		if !success {
			_ = owned.Close()
		}
	}()

	subLayerKey, err := windows.GenerateGUID()
	if err != nil {
		return nil, err
	}
	subLayerName, _ := windows.UTF16PtrFromString("HypoMux DNS egress exemption")
	subLayerDescription, _ := windows.UTF16PtrFromString(
		"High-priority dynamic sublayer for HypoMux bound DNS sockets only",
	)
	subLayerData := subLayer{
		SubLayerKey: subLayerKey,
		DisplayData: displayData{Name: subLayerName, Description: subLayerDescription},
		Weight:      0xFFFF,
	}
	if err := wfp.call(
		wfp.subLayerAdd,
		"FwpmSubLayerAdd0",
		uintptr(engine),
		uintptr(unsafe.Pointer(&subLayerData)),
		0,
	); err != nil {
		return nil, err
	}

	pathPointer, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return nil, err
	}
	var appID *byteBlob
	if err := wfp.call(
		wfp.getAppID,
		"FwpmGetAppIdFromFileName0",
		uintptr(unsafe.Pointer(pathPointer)),
		uintptr(unsafe.Pointer(&appID)),
	); err != nil {
		return nil, err
	}
	if appID != nil {
		defer wfp.free(uintptr(unsafe.Pointer(&appID)))
	}

	if err := wfp.call(wfp.transactionBegin, "FwpmTransactionBegin0", uintptr(engine), 0); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _, _ = wfp.transactionAbort.Call(uintptr(engine))
		}
	}()
	for _, rule := range rules {
		id, err := wfp.addDNSFilter(engine, subLayerKey, appID, rule)
		if err != nil {
			return nil, err
		}
		owned.filterIDs = append(owned.filterIDs, id)
	}
	if err := wfp.call(wfp.transactionCommit, "FwpmTransactionCommit0", uintptr(engine)); err != nil {
		return nil, err
	}
	committed = true
	success = true
	runtime.KeepAlive(sessionData)
	runtime.KeepAlive(subLayerData)
	return owned, nil
}

func buildRules(adapters []Adapter) []dnsRule {
	result := make([]dnsRule, 0, len(adapters)*2)
	seen := make(map[string]struct{}, len(adapters)*2)
	for _, adapter := range adapters {
		ip := net.ParseIP(strings.TrimSpace(adapter.SourceIP)).To4()
		if ip == nil || adapter.IfIndex == 0 {
			continue
		}
		for _, protocol := range []uint8{ipProtoUDP, ipProtoTCP} {
			key := fmt.Sprintf("%s/%d/%d", ip.String(), adapter.IfIndex, protocol)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, dnsRule{
				adapter:  adapter.Name,
				sourceIP: binary.LittleEndian.Uint32(ip),
				ifIndex:  adapter.IfIndex,
				protocol: protocol,
			})
		}
	}
	return result
}

func (wfp *api) addDNSFilter(
	engine windows.Handle,
	subLayerKey windows.GUID,
	appID *byteBlob,
	rule dnsRule,
) (uint64, error) {
	conditions := [5]filterCondition{
		makeCondition(conditionALEAppID, fwpByteBlobType, uintptr(unsafe.Pointer(appID))),
		makeCondition(conditionLocalAddress, fwpUint32, uintptr(rule.sourceIP)),
		makeCondition(conditionInterface, fwpUint32, uintptr(rule.ifIndex)),
		makeCondition(conditionIPProtocol, fwpUint8, uintptr(rule.protocol)),
		makeCondition(conditionRemotePort, fwpUint16, uintptr(53)),
	}
	filterKey, err := windows.GenerateGUID()
	if err != nil {
		return 0, err
	}
	protocol := "UDP"
	if rule.protocol == ipProtoTCP {
		protocol = "TCP"
	}
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf(
		"HypoMux %s DNS %s if%d",
		protocol,
		rule.adapter,
		rule.ifIndex,
	))
	description, _ := windows.UTF16PtrFromString(
		"Permit only HypoMux Core bound upstream DNS sockets through strict route",
	)
	filterData := filter{
		FilterKey:           filterKey,
		DisplayData:         displayData{Name: name, Description: description},
		Flags:               fwpmFilterFlagClearActionRight,
		LayerKey:            layerALEAuthConnectV4,
		SubLayerKey:         subLayerKey,
		Weight:              value{Type: fwpUint8, Value: 0x0F},
		NumFilterConditions: uint32(len(conditions)),
		FilterCondition:     &conditions[0],
		Action:              action{Type: fwpActionPermit},
	}
	var filterID uint64
	if err := wfp.call(
		wfp.filterAdd,
		"FwpmFilterAdd0",
		uintptr(engine),
		uintptr(unsafe.Pointer(&filterData)),
		0,
		uintptr(unsafe.Pointer(&filterID)),
	); err != nil {
		return 0, err
	}
	runtime.KeepAlive(conditions)
	runtime.KeepAlive(filterData)
	return filterID, nil
}

func makeCondition(field windows.GUID, valueType uint32, raw uintptr) filterCondition {
	return filterCondition{
		FieldKey:       field,
		MatchType:      fwpMatchEqual,
		ConditionValue: value{Type: valueType, Value: raw},
	}
}

func (session *dnsSession) FilterIDs() []uint64 {
	session.mu.Lock()
	defer session.mu.Unlock()
	return append([]uint64(nil), session.filterIDs...)
}

func (session *dnsSession) Close() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.engine == 0 {
		session.filterIDs = nil
		return nil
	}
	wfp := newAPI()
	status, _, _ := wfp.engineClose.Call(uintptr(session.engine))
	session.engine = 0
	session.filterIDs = nil
	if status != 0 {
		return fmt.Errorf("FwpmEngineClose0 failed (0x%08X)", uint32(status))
	}
	return nil
}

func newAPI() *api {
	library := windows.NewLazySystemDLL("fwpuclnt.dll")
	return &api{
		engineOpen:        library.NewProc("FwpmEngineOpen0"),
		engineClose:       library.NewProc("FwpmEngineClose0"),
		subLayerAdd:       library.NewProc("FwpmSubLayerAdd0"),
		getAppID:          library.NewProc("FwpmGetAppIdFromFileName0"),
		transactionBegin:  library.NewProc("FwpmTransactionBegin0"),
		transactionCommit: library.NewProc("FwpmTransactionCommit0"),
		transactionAbort:  library.NewProc("FwpmTransactionAbort0"),
		filterAdd:         library.NewProc("FwpmFilterAdd0"),
		freeMemory:        library.NewProc("FwpmFreeMemory0"),
	}
}

func (wfp *api) call(procedure *windows.LazyProc, name string, args ...uintptr) error {
	status, _, _ := procedure.Call(args...)
	if status == 0 {
		return nil
	}
	err := syscall.Errno(status)
	return fmt.Errorf("%s failed (0x%08X): %s", name, uint32(status), err.Error())
}

func (wfp *api) free(pointer uintptr) {
	if pointer != 0 {
		_, _, _ = wfp.freeMemory.Call(pointer)
	}
}

func mustGUID(value string) windows.GUID {
	guid, err := windows.GUIDFromString("{" + value + "}")
	if err != nil {
		panic(err)
	}
	return guid
}
