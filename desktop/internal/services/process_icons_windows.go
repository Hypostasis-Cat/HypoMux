//go:build windows

package services

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"sync"
	"syscall"
	"unsafe"
)

const (
	processIconSize     = 32
	shgfiIcon           = 0x00000100
	shgfiSmallIcon      = 0x00000001
	shgfiUseFileAttrs   = 0x00000010
	fileAttributeNormal = 0x00000080
	dibRGBColors        = 0
	diNormal            = 0x0003
	biRGB               = 0
)

type shellFileInfo struct {
	icon        uintptr
	iconIndex   int32
	attributes  uint32
	displayName [260]uint16
	typeName    [80]uint16
}

type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

var (
	processShell32            = syscall.NewLazyDLL("shell32.dll")
	processUser32             = syscall.NewLazyDLL("user32.dll")
	processGDI32              = syscall.NewLazyDLL("gdi32.dll")
	processSHGetFileInfoW     = processShell32.NewProc("SHGetFileInfoW")
	processDestroyIcon        = processUser32.NewProc("DestroyIcon")
	processDrawIconEx         = processUser32.NewProc("DrawIconEx")
	processCreateCompatibleDC = processGDI32.NewProc("CreateCompatibleDC")
	processDeleteDC           = processGDI32.NewProc("DeleteDC")
	processCreateDIBSection   = processGDI32.NewProc("CreateDIBSection")
	processSelectObject       = processGDI32.NewProc("SelectObject")
	processDeleteObject       = processGDI32.NewProc("DeleteObject")
	processIconCache          sync.Map
	processGenericIconOnce    sync.Once
	processGenericIconPixels  []byte
)

func processIconDataURL(path string) string {
	if path == "" {
		return ""
	}
	if cached, ok := processIconCache.Load(path); ok {
		return cached.(string)
	}
	icon := loadProcessIcon(path, false)
	if icon == nil || isGenericProcessIcon(icon) {
		processIconCache.Store(path, "")
		return ""
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, icon); err != nil {
		processIconCache.Store(path, "")
		return ""
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	processIconCache.Store(path, dataURL)
	return dataURL
}

func isGenericProcessIcon(icon *image.NRGBA) bool {
	processGenericIconOnce.Do(func() {
		generic := loadProcessIcon("hypomux-generic-process.exe", true)
		if generic != nil {
			processGenericIconPixels = append([]byte(nil), generic.Pix...)
		}
	})
	return len(processGenericIconPixels) > 0 && bytes.Equal(icon.Pix, processGenericIconPixels)
}

func loadProcessIcon(path string, useFileAttributes bool) *image.NRGBA {
	pathPointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	flags := uintptr(shgfiIcon | shgfiSmallIcon)
	attributes := uintptr(0)
	if useFileAttributes {
		flags |= shgfiUseFileAttrs
		attributes = fileAttributeNormal
	}
	var info shellFileInfo
	result, _, _ := processSHGetFileInfoW.Call(
		uintptr(unsafe.Pointer(pathPointer)),
		attributes,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		flags,
	)
	if result == 0 || info.icon == 0 {
		return nil
	}
	defer processDestroyIcon.Call(info.icon)
	return renderProcessIcon(info.icon)
}

func renderProcessIcon(icon uintptr) *image.NRGBA {
	black := drawProcessIcon(icon, 0)
	white := drawProcessIcon(icon, 255)
	if black == nil || white == nil {
		return nil
	}
	result := image.NewNRGBA(image.Rect(0, 0, processIconSize, processIconSize))
	for offset := 0; offset < len(black); offset += 4 {
		blueBlack, greenBlack, redBlack := int(black[offset]), int(black[offset+1]), int(black[offset+2])
		blueWhite, greenWhite, redWhite := int(white[offset]), int(white[offset+1]), int(white[offset+2])
		alpha := 255 - max(redWhite-redBlack, greenWhite-greenBlack, blueWhite-blueBlack)
		if alpha < 0 {
			alpha = 0
		}
		if alpha > 255 {
			alpha = 255
		}
		if alpha == 0 {
			continue
		}
		result.Pix[offset] = uint8(min(255, (redBlack*255+alpha/2)/alpha))
		result.Pix[offset+1] = uint8(min(255, (greenBlack*255+alpha/2)/alpha))
		result.Pix[offset+2] = uint8(min(255, (blueBlack*255+alpha/2)/alpha))
		result.Pix[offset+3] = uint8(alpha)
	}
	return result
}

func drawProcessIcon(icon uintptr, background byte) []byte {
	dc, _, _ := processCreateCompatibleDC.Call(0)
	if dc == 0 {
		return nil
	}
	defer processDeleteDC.Call(dc)
	bitmapInfo := bitmapInfo{header: bitmapInfoHeader{
		size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		width:       processIconSize,
		height:      -processIconSize,
		planes:      1,
		bitCount:    32,
		compression: biRGB,
	}}
	var pixels unsafe.Pointer
	bitmap, _, _ := processCreateDIBSection.Call(
		dc,
		uintptr(unsafe.Pointer(&bitmapInfo)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&pixels)),
		0,
		0,
	)
	if bitmap == 0 || pixels == nil {
		return nil
	}
	defer processDeleteObject.Call(bitmap)
	previous, _, _ := processSelectObject.Call(dc, bitmap)
	if previous == 0 {
		return nil
	}
	defer processSelectObject.Call(dc, previous)
	buffer := unsafe.Slice((*byte)(pixels), processIconSize*processIconSize*4)
	for offset := 0; offset < len(buffer); offset += 4 {
		buffer[offset] = background
		buffer[offset+1] = background
		buffer[offset+2] = background
		buffer[offset+3] = 255
	}
	success, _, _ := processDrawIconEx.Call(
		dc,
		0,
		0,
		icon,
		processIconSize,
		processIconSize,
		0,
		0,
		diNormal,
	)
	if success == 0 {
		return nil
	}
	return append([]byte(nil), buffer...)
}
