//go:build windows

package services

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const trustedInstallerPublisher = "SignPath Foundation"

var (
	wintrustDLL                         = windows.NewLazySystemDLL("wintrust.dll")
	wtHelperProvDataFromStateData       = wintrustDLL.NewProc("WTHelperProvDataFromStateData")
	wtHelperGetProvSignerFromChain      = wintrustDLL.NewProc("WTHelperGetProvSignerFromChain")
	wtHelperGetProvCertificateFromChain = wintrustDLL.NewProc("WTHelperGetProvCertFromChain")
)

// cryptProviderCertificate only declares the leading fields of
// CRYPT_PROVIDER_CERT that are required here. Windows owns the pointed-to
// memory until the WinVerifyTrust state is closed.
type cryptProviderCertificate struct {
	Size        uint32
	Certificate *windows.CertContext
}

func verifyDownloadedInstallerAuthenticity(installerPath string) error {
	filePath, err := windows.UTF16PtrFromString(installerPath)
	if err != nil {
		return errors.New("安装包路径无效")
	}
	fileInfo := &windows.WinTrustFileInfo{
		Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
		FilePath: filePath,
	}
	trustData := &windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(fileInfo),
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		ProvFlags:                       windows.WTD_CACHE_ONLY_URL_RETRIEVAL | windows.WTD_SAFER_FLAG,
		UIContext:                       windows.WTD_UICONTEXT_INSTALL,
	}
	verifyErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		trustData,
	)
	defer func() {
		trustData.StateAction = windows.WTD_STATEACTION_CLOSE
		_ = windows.WinVerifyTrustEx(
			windows.InvalidHWND,
			&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
			trustData,
		)
	}()
	if verifyErr != nil {
		return fmt.Errorf("Windows 不信任安装包签名：%w", verifyErr)
	}
	if trustData.StateData == 0 {
		return errors.New("Windows 未返回签名验证状态")
	}

	providerData, _, _ := wtHelperProvDataFromStateData.Call(uintptr(trustData.StateData))
	if providerData == 0 {
		return errors.New("无法读取安装包签名提供程序数据")
	}
	signer, _, _ := wtHelperGetProvSignerFromChain.Call(providerData, 0, 0, 0)
	if signer == 0 {
		return errors.New("安装包没有主签名者")
	}
	providerCertificate, _, _ := wtHelperGetProvCertificateFromChain.Call(signer, 0)
	if providerCertificate == 0 {
		return errors.New("无法读取安装包签名证书")
	}
	certificateRecord, err := readCryptProviderCertificate(providerCertificate)
	if err != nil {
		return err
	}
	certificate := certificateRecord.Certificate
	if certificate == nil {
		return errors.New("安装包签名证书为空")
	}
	publisher, err := certificateSimpleDisplayName(certificate)
	if err != nil {
		return err
	}
	if publisher != trustedInstallerPublisher {
		return fmt.Errorf("安装包发布者不受信任：%q", publisher)
	}
	return nil
}

func readCryptProviderCertificate(address uintptr) (cryptProviderCertificate, error) {
	// WinTrust owns this memory until WTD_STATEACTION_CLOSE. ReadProcessMemory
	// copies the two fields we need without treating a Windows uintptr as a Go
	// pointer, keeping both checkptr and go vet guarantees intact.
	var certificate cryptProviderCertificate
	var bytesRead uintptr
	if err := windows.ReadProcessMemory(
		windows.CurrentProcess(),
		address,
		(*byte)(unsafe.Pointer(&certificate)),
		unsafe.Sizeof(certificate),
		&bytesRead,
	); err != nil {
		return cryptProviderCertificate{}, fmt.Errorf("读取安装包签名证书失败：%w", err)
	}
	if bytesRead != unsafe.Sizeof(certificate) {
		return cryptProviderCertificate{}, errors.New("安装包签名证书数据不完整")
	}
	return certificate, nil
}

func certificateSimpleDisplayName(certificate *windows.CertContext) (string, error) {
	characters := windows.CertGetNameString(
		certificate,
		windows.CERT_NAME_SIMPLE_DISPLAY_TYPE,
		0,
		nil,
		nil,
		0,
	)
	if characters <= 1 {
		return "", errors.New("无法读取安装包发布者名称")
	}
	buffer := make([]uint16, characters)
	written := windows.CertGetNameString(
		certificate,
		windows.CERT_NAME_SIMPLE_DISPLAY_TYPE,
		0,
		nil,
		&buffer[0],
		uint32(len(buffer)),
	)
	if written <= 1 {
		return "", errors.New("读取安装包发布者名称失败")
	}
	return windows.UTF16ToString(buffer), nil
}
