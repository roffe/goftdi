//go:build windows
// +build windows

package ftdi

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"time"
	"unsafe"
)

var ErrInit error

var (
	ftCreateDeviceInfoList *syscall.Proc
	ftGetDeviceInfoDetail  *syscall.Proc
	ftOpen                 *syscall.Proc
	ftClose                *syscall.Proc
	ftRead                 *syscall.Proc
	ftWrite                *syscall.Proc
	ftGetStatus            *syscall.Proc
	ftGetQueueStatus       *syscall.Proc
	ftPurge                *syscall.Proc
	ftSetBaudRate          *syscall.Proc
	ftSetBitMode           *syscall.Proc
	ftSetFlowControl       *syscall.Proc
	ftSetLatency           *syscall.Proc
	ftSetChars             *syscall.Proc
	ftSetLineProperty      *syscall.Proc
	ftSetTimeout           *syscall.Proc
	ftSetTransferSize      *syscall.Proc
	ftResetPort            *syscall.Proc
	ftResetDevice          *syscall.Proc
	ftSetBreakOn           *syscall.Proc
	ftSetBreakOff          *syscall.Proc
)

var (
	ErrInvalidDriver  = errors.New("Unsupported FTDI DLL")
	ErrDriverNotFound = errors.New("FTDI driver not found in system directories")
)

func init() {
	dllFuncs := map[string]**syscall.Proc{
		"FT_CreateDeviceInfoList":   &ftCreateDeviceInfoList,
		"FT_GetDeviceInfoDetail":    &ftGetDeviceInfoDetail,
		"FT_Open":                   &ftOpen,
		"FT_Close":                  &ftClose,
		"FT_Read":                   &ftRead,
		"FT_Write":                  &ftWrite,
		"FT_GetStatus":              &ftGetStatus,
		"FT_GetQueueStatus":         &ftGetQueueStatus,
		"FT_Purge":                  &ftPurge,
		"FT_SetBaudRate":            &ftSetBaudRate,
		"FT_SetBitMode":             &ftSetBitMode,
		"FT_SetFlowControl":         &ftSetFlowControl,
		"FT_SetLatencyTimer":        &ftSetLatency,
		"FT_SetChars":               &ftSetChars,
		"FT_SetDataCharacteristics": &ftSetLineProperty,
		"FT_SetTimeouts":            &ftSetTimeout,
		"FT_SetUSBParameters":       &ftSetTransferSize,
		"FT_ResetPort":              &ftResetPort,
		"FT_ResetDevice":            &ftResetDevice,
		"FT_SetBreakOn":             &ftSetBreakOn,
		"FT_SetBreakOff":            &ftSetBreakOff,
	}
	d2xx, err := syscall.LoadDLL("ftd2xx.dll")
	if err != nil {
		ErrInit = ErrDriverNotFound
		return
	}
	for k, v := range dllFuncs {
		proc, err := d2xx.FindProc(k)
		if err != nil {
			ErrInit = ErrInvalidDriver
			return
		}
		*v = proc
	}
}

func bytesToString(b []byte) string {
	n := bytes.Index(b, []byte{0})
	return string(b[:n])
}

type Device uintptr

type DeviceInfo struct {
	index        uint64
	flags        uint64
	dtype        uint64
	id           uint64
	location     uint64
	SerialNumber string
	Description  string
	handle       uintptr
}

func GetDeviceList() (di []DeviceInfo, e error) {
	var n uint32
	r, _, _ := ftCreateDeviceInfoList.Call(uintptr(unsafe.Pointer(&n)))
	if r != FT_OK {
		return nil, ftdiError(r)
	}

	di = make([]DeviceInfo, n)
	for i := uint32(0); i < n; i++ {
		var d DeviceInfo
		var sn [16]byte
		var description [64]byte
		d.index = uint64(i)
		r, _, _ = ftGetDeviceInfoDetail.Call(uintptr(i),
			uintptr(unsafe.Pointer(&(d.flags))),
			uintptr(unsafe.Pointer(&d.dtype)),
			uintptr(unsafe.Pointer(&d.id)),
			uintptr(unsafe.Pointer(&d.location)),
			uintptr(unsafe.Pointer(&sn)),
			uintptr(unsafe.Pointer(&description)),
			uintptr(unsafe.Pointer(&d.handle)))
		if r != FT_OK {
			n--
			di = di[:n]
			continue
		}
		d.SerialNumber = bytesToString(sn[:])
		d.Description = bytesToString(description[:])

		di[i] = d
	}
	return di, nil
}

func Open(di DeviceInfo) (*Device, error) {
	var dev Device
	r, _, _ := ftOpen.Call(uintptr(di.index), uintptr(unsafe.Pointer(&dev)))
	if r != FT_OK {
		return nil, ftdiError(r)
	}
	return &dev, nil
}

func (d *Device) Close() (e error) {
	r, _, _ := ftClose.Call(uintptr(*d))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) GetStatus() (rx_queue, tx_queue, events int32, e error) {
	r, _, _ := ftGetStatus.Call(uintptr(*d),
		uintptr(unsafe.Pointer(&rx_queue)),
		uintptr(unsafe.Pointer(&tx_queue)),
		uintptr(unsafe.Pointer(&events)))

	if r != FT_OK {
		return rx_queue, tx_queue, events, ftdiError(r)
	}
	return rx_queue, tx_queue, events, nil
}

func (d *Device) GetQueueStatus() (rx_queue int32, e error) {
	r, _, _ := ftGetQueueStatus.Call(uintptr(*d),
		uintptr(unsafe.Pointer(&rx_queue)))

	if r != FT_OK {
		return rx_queue, ftdiError(r)
	}
	return rx_queue, nil
}

func (d *Device) Read(p []byte) (n int, e error) {
	var bytesRead uint32
	bytesToRead := uint32(len(p))

	for {
		rx_cnt, err := d.GetQueueStatus()
		if err != nil {
			return int(bytesRead), io.EOF
		}
		if rx_cnt > 0 {
			bytesToRead = uint32(rx_cnt)
			break
		}
		time.Sleep(CHECK_RX_DELAY_MS * time.Millisecond)
	}

	ptr := &p[0] //A reference to the first element of the underlying "array"
	r, _, _ := ftRead.Call(uintptr(*d),
		uintptr(unsafe.Pointer(ptr)),
		uintptr(bytesToRead),
		uintptr(unsafe.Pointer(&bytesRead)))
	if r != FT_OK {
		return int(bytesRead), ftdiError(r)
	}
	return int(bytesRead), nil
}

func (d *Device) Write(p []byte) (n int, e error) {
	var bytesWritten uint32
	bytesToWrite := uint32(len(p))
	ptr := &p[0] //A reference to the first element of the underlying "array"
	r, _, _ := ftWrite.Call(uintptr(*d),
		uintptr(unsafe.Pointer(ptr)),
		uintptr(bytesToWrite),
		uintptr(unsafe.Pointer(&bytesWritten)))

	if r != FT_OK {
		return int(bytesWritten), ftdiError(r)
	}
	return int(bytesWritten), nil
}

func (d *Device) SetBaudRate(baud uint) (e error) {
	r, _, _ := ftSetBaudRate.Call(uintptr(*d), uintptr(uint32(baud)))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

// Set the 'event' and 'error' characheters. Disabled if the charachter is '0x00'.
func (d *Device) SetChars(event, err byte) (e error) {
	r, _, _ := ftSetChars.Call(uintptr(*d),
		uintptr(event),
		uintptr(event),
		uintptr(err),
		uintptr(err))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) SetBitMode(mode BitMode) (e error) {
	r, _, _ := ftSetBitMode.Call(uintptr(*d),
		uintptr(0x00), // All pins set to input
		uintptr(byte(mode)))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) SetFlowControl(f FlowControl) (e error) {
	r, _, _ := ftSetFlowControl.Call(uintptr(*d),
		uintptr(uint16(f)), // All pins set to input
		uintptr(0x11),      // XON Character
		uintptr(0x13))      // XOFF Character
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

// Set latency in milliseconds. Valid between 2 and 255.
func (d *Device) SetLatency(latency int) (e error) {
	r, _, _ := ftSetLatency.Call(uintptr(*d), uintptr(byte(latency)))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

// Set the transfer size. Valid between 64 and 64k bytes in 64-byte increments.
func (d *Device) SetTransferSize(read_size, write_size int) (e error) {
	r, _, _ := ftSetTransferSize.Call(uintptr(*d),
		uintptr(uint32(read_size)),
		uintptr(uint32(write_size)))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) SetLineProperty(props LineProperties) (e error) {
	r, _, _ := ftSetLineProperty.Call(uintptr(*d),
		uintptr(byte(props.Bits)),
		uintptr(byte(props.StopBits)),
		uintptr(byte(props.Parity)))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) SetTimeout(read_timeout, write_timeout int) (e error) {
	r, _, _ := ftSetTimeout.Call(uintptr(*d),
		uintptr(uint32(read_timeout)),
		uintptr(uint32(write_timeout)))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) Reset() (e error) {
	r, _, _ := ftResetDevice.Call(uintptr(*d))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) Purge() (e error) {
	// Purge both RX and TX buffers
	r, _, _ := ftPurge.Call(uintptr(*d), uintptr(0x01|0x02))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) SetBreakOn(props LineProperties) (e error) {
	r, _, _ := ftSetBreakOn.Call(uintptr(*d))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}

func (d *Device) SetBreakOff(props LineProperties) (e error) {
	r, _, _ := ftSetBreakOff.Call(uintptr(*d))
	if r != FT_OK {
		return ftdiError(r)
	}
	return nil
}
